package server

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// 本文件实现「模组预更新」：单独跑 -only_update_server_mods，把缺失的
// 创意工坊模组下载到共享 ugc 目录。
//
// 为什么要有这一步：面板给所有分片的启动参数都带 -skip_update_server_mods，
// 服务器启动时不再自己补模组。这样收敛是为了躲开官方文档明确提到的并发坑——
// 多个分片同时启动会抢着往同一个共享 ugc 目录写，最坏情况是下载损坏。
// 把更新动作收敛成「启动前单独跑，更新完立即退出」，就只有一个写入者。
//
// 两个参数的行为（Klei 官方语义）：
//   -only_update_server_mods   检查并更新模组，然后立即退出（不启动世界）
//   -skip_update_server_mods   跳过模组更新，直接启动
//
// ⚠️ 引擎的坑（本机实测，bin64/logs/workshop_log.txt 可复盘）：
// 下载阶段只要超过约 30 秒没有任何回调，DST 就直接打印
// "FinishDownloadingServerMods Complete" 并退出——哪怕 13 个里只完成了 1 个，
// 退出码还是 0。而下载前要先现查模组列表（日志里的
// "missing content info for item ..."），一查就容易超 30 秒。
// 所以**单轮不可信**：这里必须循环执行，每轮跑完重新清点缺失数，
// 有进展就继续，直到清零或没有进展为止。

const (
	// ModUpdateTimeout 是一次更新会话（可能包含多轮进程）的最长等待时间。
	ModUpdateTimeout = time.Hour
	// maxModRuns 是一次会话最多允许的进程轮数。
	// 每轮至少能推进一个模组，轮数按缺失模组数给出足够余量。
	maxModRuns = 24
	// maxSingleModRuns 是单模组模式的最多轮数。
	// 引擎的「30 秒无回调提前收工」抽风可能让单轮也下不完，
	// 重试通常能续上（Steam 断点续传），但连续 3 轮失败就该停了。
	maxSingleModRuns = 3
	// modsUpdateLogName 是更新会话的落盘日志（logs/ 下），出问题靠它复盘。
	// 之前更新进程的输出只存在内存里，页面一刷新就没了，
	// 导致「瞬间完成但什么都没下」这类问题无从排查。
	modsUpdateLogName = "mods_update.log"
	// downloadDoneMark 是引擎走完下载阶段的标志行。
	downloadDoneMark = "FinishDownloadingServerMods"
)

// modOutputLines 是保留给前端展示的最大输出行数，超出后丢最旧的。
const modOutputLines = 400

// percentRe 从服务器输出里提取下载百分比（部分版本会打印）。
var percentRe = regexp.MustCompile(`(\d{1,3})\s*%`)

// ModsChecker 返回当前仍缺失的工坊模组 ID 列表。
// 由 Web 层注入（汇总所有房间启用但未下载的模组），更新循环据此判断
// 是否继续下一轮。返回错误表示暂时无法清点，循环会退化为单轮。
type ModsChecker func() ([]string, error)

// ModsSetupSwap 供单模组更新使用：把安装级下载清单临时换成只含目标模组，
// 返回的 restore 在会话结束时恢复所有房间的并集。由 Web 层注入。
type ModsSetupSwap func(target string) (restore func(), err error)

// ModsUpdate 是模组更新的进度视图，供 Web 层轮询。
type ModsUpdate struct {
	Running  bool     `json:"running"`
	Progress string   `json:"progress"`
	Error    string   `json:"error"`
	Output   []string `json:"output"`
}

// SetUgcDir 设置共享工坊模组目录（-ugc_directory 的值）。
// 必须在启动任何分片之前调用；为空表示沿用服务器的默认布局。
func (m *Manager) SetUgcDir(dir string) {
	m.mu.Lock()
	m.ugcDir = dir
	m.mu.Unlock()
}

// SetModsChecker 注入缺失模组的清点逻辑。必须在更新前设置，
// 否则更新循环无法感知进度，只会跑一轮。
func (m *Manager) SetModsChecker(fn ModsChecker) {
	m.mu.Lock()
	m.modsChecker = fn
	m.mu.Unlock()
}

// SetModsSetupSwap 注入下载清单交换逻辑（单模组更新必需）。
func (m *Manager) SetModsSetupSwap(fn ModsSetupSwap) {
	m.mu.Lock()
	m.modsSetupSwap = fn
	m.mu.Unlock()
}

// baseArgs 返回定位存档与共享模组目录的基础参数，不含端口。
// 启动分片与模组预更新共用，保证两边对「存档在哪、模组在哪」的理解永远一致。
func (m *Manager) baseArgs(c *Cluster, shard string) []string {
	args := []string{
		"-persistent_storage_root", m.baseDir,
		"-conf_dir", c.ConfDir,
		"-cluster", c.Key(),
		"-shard", shard,
	}
	// 共享 ugc 目录：所有房间、所有分片读同一份模组实体。
	// 用绝对路径——相对路径要依赖「工作目录是 bin/」这个隐含约定，太脆。
	if m.ugcDir != "" {
		args = append(args, "-ugc_directory", m.ugcDir)
	}
	return args
}

// UpdateMods 启动一次模组更新会话（异步，立即返回）。
//
// target 为空：全量模式，连续跑多轮更新进程直到所有缺失模组清零；
// target 为某个工坊模组（workshop-<数字>）：单模组模式，临时把下载清单
// 换成只含目标模组，已安装的先删旧文件再重下（强制刷新），
// 未安装的只下载它，会话结束后清单自动恢复并集。
//
// 指定的房间与分片只用来确定存档位置，下载内容来自安装级的
// dedicated_server_mods_setup.lua 与该分片的 modoverrides.lua。
func (m *Manager) UpdateMods(clusterKey, shard, target string) error {
	if shard == "" {
		shard = "Master"
	}
	num := ""
	if target != "" {
		num = strings.TrimPrefix(target, "workshop-")
		if num == "" || !isDigits(num) {
			return fmt.Errorf("仅支持单独更新创意工坊模组（workshop-<数字>）: %s", target)
		}
	}
	c, ok := m.GetCluster(clusterKey)
	if !ok {
		return fmt.Errorf("房间不存在: %s", clusterKey)
	}
	if m.ugcDir == "" {
		return fmt.Errorf("未配置共享模组目录，无法预更新模组")
	}

	m.modMu.Lock()
	if m.modRunning {
		m.modMu.Unlock()
		return fmt.Errorf("模组更新正在进行中，请等待完成")
	}

	// Windows 下模组文件被运行中的服务器占用，而且 acf 清单也会被写，
	// 因此必须全部停服后再更新
	if busy := m.RunningShards(); len(busy) > 0 {
		m.modMu.Unlock()
		return fmt.Errorf("有分片正在运行（%s），请先全部停止再更新模组",
			strings.Join(busy, "、"))
	}

	exePath, workDir := m.resolveExe()
	if _, err := os.Stat(exePath); err != nil {
		m.modMu.Unlock()
		return fmt.Errorf("未找到服务器可执行文件: %s（请先安装专用服务器）", exePath)
	}
	if target != "" && m.modsSetupSwap == nil {
		m.modMu.Unlock()
		return fmt.Errorf("单模组更新所需的下载清单交换回调未注入")
	}

	m.modRunning = true
	m.modCancel = false
	m.modErr = ""
	if target != "" {
		m.modOutput = []string{fmt.Sprintf("开始单独更新模组 workshop-%s（已安装的会先删旧文件再重下）…", num)}
	} else {
		m.modOutput = []string{"开始更新模组（引擎每轮可能只下载一两个，会自动多跑几轮）…"}
	}
	m.modMu.Unlock()

	go m.runModsSession(c, shard, exePath, workDir, num)
	return nil
}

// isDigits 判断字符串是否全为十进制数字（工坊 ID 的格式）。
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// targetModDir 返回单模组更新目标在本机共享目录下的实体目录。
func (m *Manager) targetModDir(num string) string {
	return filepath.Join(m.ugcDir, "content", "322330", num)
}

// targetModInstalled 判断目标模组是否已有完整本地副本。
// 以 modinfo.lua 为准：下载中途留下的残缺目录不算。
func (m *Manager) targetModInstalled(num string) bool {
	_, err := os.Stat(filepath.Join(m.targetModDir(num), "modinfo.lua"))
	return err == nil
}

// runModsSession 执行完整的更新会话：多轮进程 + 每轮结束清点缺失数。
//
// num 非空表示单模组模式：临时交换下载清单、先删旧副本再重下，
// 完成判定只看目标模组是否就位。
func (m *Manager) runModsSession(c *Cluster, shard, exePath, workDir, num string) {
	var logFile *os.File
	if f, err := os.OpenFile(filepath.Join(m.logDir, modsUpdateLogName),
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644); err == nil {
		logFile = f
	}
	defer func() {
		if logFile != nil {
			_ = logFile.Close()
		}
		m.modMu.Lock()
		m.modRunning = false
		m.modCmd = nil
		m.modMu.Unlock()
	}()

	log := func(format string, args ...interface{}) {
		line := fmt.Sprintf(format, args...)
		m.appendModOutput(line)
		if logFile != nil {
			fmt.Fprintln(logFile, time.Now().Format("15:04:05")+"  "+line)
		}
	}

	// 单模组模式：先删旧副本（强制刷新），再把下载清单临时换成只含目标。
	// restore 必须在所有退出路径上执行，否则清单会停留在缺别的模组的状态。
	restore := func() {}
	if num != "" {
		if m.targetModInstalled(num) {
			log("workshop-%s 已有本地副本，先删除旧文件再重新下载（强制刷新）", num)
			if err := os.RemoveAll(m.targetModDir(num)); err != nil {
				m.setModErr(fmt.Sprintf("删除旧副本失败: %v", err))
				return
			}
		}
		swap, ok := m.getModsSetupSwap()
		if !ok {
			m.setModErr("单模组更新所需的下载清单交换回调未注入")
			return
		}
		r, err := swap("workshop-" + num)
		if err != nil {
			m.setModErr(fmt.Sprintf("临时改写下载清单失败: %v", err))
			return
		}
		restore = r
		defer restore()
		log("已切换下载清单为单模组模式，结束后自动恢复全部房间的并集")
	}

	// 没有清点回调就无法判断进度，退化为单轮（宁可少跑也不盲目循环）
	runs := 1
	switch {
	case num != "":
		runs = maxSingleModRuns
	case m.modsChecker != nil:
		runs = maxModRuns
	}

	deadline := time.Now().Add(ModUpdateTimeout)
	prev := -1
	for attempt := 1; attempt <= runs; attempt++ {
		if num != "" {
			// 单模组模式：完成判定只看目标模组；一轮没下完就再试
			// （Steam 断点续传），连续失败到轮数上限才放弃
			if m.targetModInstalled(num) {
				log("workshop-%s 已下载完成，更新完成", num)
				return
			}
			if prev >= 0 {
				log("第 %d 轮结束，目标模组仍未就位，继续重试", attempt-1)
			}
			log("第 %d 轮开始，目标 workshop-%s", attempt, num)
		} else {
			missing, hasCheck := m.missingModCount()
			if hasCheck {
				if missing == 0 {
					log("全部模组均已下载，更新完成")
					return
				}
				if prev >= 0 && missing >= prev {
					log("本轮没有下载进展（剩余 %d 个），停止重试", missing)
					m.setModErr(fmt.Sprintf("仍有 %d 个模组未下载，可稍后再次点击「更新模组」继续", missing))
					return
				}
				prev = missing
				log("第 %d 轮开始，待下载 %d 个", attempt, missing)
			}
		}
		if !time.Now().Before(deadline) {
			m.setModErr("更新会话超过最长等待时间，已中止")
			return
		}

		exitErr, canceled, ranDownload := m.runUpdaterOnce(c, shard, exePath, workDir, deadline, log)
		if canceled {
			m.setModErr("已手动中止")
			return
		}
		if exitErr != nil {
			m.setModErr(fmt.Sprintf("更新进程异常退出（%v），详见 logs/%s", exitErr, modsUpdateLogName))
			return
		}
		if !ranDownload {
			m.setModErr(fmt.Sprintf("更新进程提前退出，未进入下载阶段（详见 logs/%s）", modsUpdateLogName))
			return
		}
	}

	if num != "" {
		if !m.targetModInstalled(num) {
			m.setModErr(fmt.Sprintf("已连续更新 %d 轮，workshop-%s 仍未就位，请再次点击重试", maxModRuns, num))
		}
		return
	}
	if missing, ok := m.missingModCount(); ok && missing > 0 {
		m.setModErr(fmt.Sprintf("已连续更新 %d 轮仍有 %d 个未下载，请再次点击「更新模组」继续",
			maxModRuns, missing))
	}
}

// getModsSetupSwap 读取注入的清单交换回调。
func (m *Manager) getModsSetupSwap() (ModsSetupSwap, bool) {
	m.mu.RLock()
	fn := m.modsSetupSwap
	m.mu.RUnlock()
	return fn, fn != nil
}

// runUpdaterOnce 跑一轮更新进程，采集输出并等待退出。
//
// ranDownload 表示引擎是否真的走到了下载完成阶段——
// 引擎可能在任何阶段静默退出且退出码为 0，必须靠标志行区分。
func (m *Manager) runUpdaterOnce(
	c *Cluster, shard, exePath, workDir string, deadline time.Time,
	log func(string, ...interface{}),
) (exitErr error, canceled bool, ranDownload bool) {
	// 参数与分片启动保持一致（含 -console 与持有中的标准输入），
	// 只把「跳过更新」换成「只更新然后退出」
	cmd := exec.Command(exePath, append(m.baseArgs(c, shard),
		"-console",
		"-only_update_server_mods",
	)...)
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}

	// 持有标准输入而不是让它立刻读到 EOF：进程环境要尽量贴近
	// 能正常工作的分片启动，避免引擎因读 stdin 行为不一致而提前退出
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("创建标准输入管道失败: %w", err), false, false
	}
	defer stdin.Close()

	// stdout 与 stderr 合并进同一条管道：下载进度和报错两边都有
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动失败: %w", err), false, false
	}

	m.modMu.Lock()
	m.modCmd = cmd
	m.modMu.Unlock()

	kill := time.AfterFunc(time.Until(deadline), func() {
		_ = cmd.Process.Kill()
	})

	downloadDone := false
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if strings.Contains(line, downloadDoneMark) {
				downloadDone = true
			}
			log("%s", line)
		}
	}()

	err = cmd.Wait()
	kill.Stop()
	// 关闭写端让读取协程拿到 EOF 收尾；进程已退出，不会有新的写入
	_ = pw.Close()
	<-done

	if m.isModCanceled() {
		return nil, true, downloadDone
	}
	if !time.Now().Before(deadline) {
		return fmt.Errorf("超过最长等待时间被强制终止"), false, downloadDone
	}
	if err != nil {
		return err, false, downloadDone
	}
	return nil, false, downloadDone
}

// StopModsUpdate 中止正在进行的模组更新（当前这轮进程会被杀掉，
// 会话随即结束）。
func (m *Manager) StopModsUpdate() error {
	m.modMu.Lock()
	defer m.modMu.Unlock()
	if !m.modRunning {
		return fmt.Errorf("当前没有正在进行的模组更新")
	}
	m.modCancel = true
	if m.modCmd != nil && m.modCmd.Process != nil {
		_ = m.modCmd.Process.Kill()
	}
	return nil
}

// ModsUpdateStatus 返回当前模组更新的进度。
func (m *Manager) ModsUpdateStatus() ModsUpdate {
	m.modMu.Lock()
	defer m.modMu.Unlock()

	out := make([]string, len(m.modOutput))
	copy(out, m.modOutput)
	progress := ""
	for i := len(out) - 1; i >= 0; i-- {
		if match := percentRe.FindStringSubmatch(out[i]); match != nil {
			progress = match[1] + "%"
			break
		}
	}
	return ModsUpdate{
		Running:  m.modRunning,
		Progress: progress,
		Error:    m.modErr,
		Output:   out,
	}
}

// ModsUpdating 判断是否正在更新模组。
func (m *Manager) ModsUpdating() bool {
	m.modMu.Lock()
	defer m.modMu.Unlock()
	return m.modRunning
}

// missingModCount 清点当前缺失的工坊模组数量。
// 第二个返回值表示是否存在可用的清点逻辑。
func (m *Manager) missingModCount() (int, bool) {
	m.mu.RLock()
	fn := m.modsChecker
	m.mu.RUnlock()
	if fn == nil {
		return 0, false
	}
	ids, err := fn()
	if err != nil {
		return 0, false
	}
	return len(ids), true
}

func (m *Manager) isModCanceled() bool {
	m.modMu.Lock()
	defer m.modMu.Unlock()
	return m.modCancel
}

func (m *Manager) setModErr(msg string) {
	m.modMu.Lock()
	m.modErr = msg
	m.modMu.Unlock()
}

// appendModOutput 追加一行输出到内存缓冲与面板控制台。
func (m *Manager) appendModOutput(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	m.modMu.Lock()
	if len(m.modOutput) >= modOutputLines {
		// 丢掉最旧的一半，避免频繁切片带来的开销
		keep := modOutputLines / 2
		m.modOutput = append([]string(nil), m.modOutput[len(m.modOutput)-keep:]...)
	}
	m.modOutput = append(m.modOutput, line)
	m.modMu.Unlock()
	fmt.Println("[mods] " + line)
}
