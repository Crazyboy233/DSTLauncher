//go:build windows

package server

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// 采样分片进程的 CPU / 内存占用。
//
// 用 Win32 API 而不是引入 gopsutil：项目一直是零第三方依赖，
// 而这里只需要两个调用，标准库的 syscall 就够了。
//
// 开销：每次采样是 OpenProcess + GetProcessMemoryInfo + GetProcessTimes +
// CloseHandle，共 4 次系统调用，微秒级。采样挂在既有的状态轮询上
// （前端每 3 秒拉一次 /api/proc/status），不额外起定时器。

const (
	processQueryLimitedInformation = 0x1000
	processVMRead                  = 0x0010

	// 两次采样的最小间隔。低于它就复用上次结果：
	// CPU 占用率是「累计 CPU 时间 / 墙钟时间」的差值，
	// 间隔太短时多开几个标签页同时刷新会让数值剧烈抖动。
	minSampleInterval = 900 * time.Millisecond
)

var (
	kernel32    = syscall.NewLazyDLL("kernel32.dll")
	psapi       = syscall.NewLazyDLL("psapi.dll")
	procOpen    = kernel32.NewProc("OpenProcess")
	procTimes   = kernel32.NewProc("GetProcessTimes")
	procMemInfo = psapi.NewProc("GetProcessMemoryInfo")
)

// PROCESS_MEMORY_COUNTERS 的内存布局（64 位下两个 DWORD 之后按 8 字节对齐，无填充）
type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

// queryProcess 读取进程的累计 CPU 时间与物理内存占用（工作集）。
//
// 用的是 QUERY_LIMITED_INFORMATION | VM_READ：
// 对同用户下的子进程足够，且比 PROCESS_QUERY_INFORMATION 权限要求更低。
func queryProcess(pid int) (cpu time.Duration, memBytes uint64, err error) {
	h, _, callErr := procOpen.Call(
		uintptr(processQueryLimitedInformation|processVMRead), 0, uintptr(pid))
	if h == 0 {
		return 0, 0, callErr
	}
	defer syscall.CloseHandle(syscall.Handle(h)) //nolint:errcheck

	var mem processMemoryCounters
	mem.cb = uint32(unsafe.Sizeof(mem))
	if r, _, e := procMemInfo.Call(
		h, uintptr(unsafe.Pointer(&mem)), uintptr(mem.cb)); r == 0 {
		return 0, 0, e
	}

	var creation, exit, kernel, user syscall.Filetime
	if r, _, e := procTimes.Call(h,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user))); r == 0 {
		return 0, 0, e
	}

	cpu = filetimeSpan(kernel) + filetimeSpan(user)
	return cpu, uint64(mem.workingSetSize), nil
}

// filetimeSpan 把 GetProcessTimes 返回的 FILETIME 换算成时长。
//
// 不能用 syscall.Filetime.Nanoseconds()：那个方法的语义是「时间戳」，
// 会把基准从 1601 年换算到 1970 年（减去 116444736000000000 个 100ns 单位）。
// 而这里拿到的是**经过的时长**，套用它只会得到一个巨大的负数。
// 所以直接按 100ns 单位累加。
func filetimeSpan(ft syscall.Filetime) time.Duration {
	ticks := int64(ft.HighDateTime)<<32 | int64(ft.LowDateTime)
	return time.Duration(ticks) * 100 * time.Nanosecond
}

// procResource 刷新并返回分片的物理内存字节数与整机 CPU 占用率（%）。
//
// CPU 按「占整机」而非「占单核」口径，与任务管理器一致；
// 首次采样没有基准，占用率返回 0。
func (m *Manager) procResource(p *process) (uint64, float64) {
	cpu, mem, err := queryProcess(p.pid)
	if err != nil {
		// 进程刚好退出、或权限不足：只丢这一次采样，不影响状态本身
		return 0, 0
	}

	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	prev := p.res
	elapsed := now.Sub(prev.at)
	if !prev.at.IsZero() && elapsed < minSampleInterval {
		return mem, prev.pct
	}

	pct := 0.0
	if !prev.at.IsZero() && elapsed > 0 {
		delta := (cpu - prev.cpu).Seconds()
		if delta < 0 {
			delta = 0 // 进程重启过（PID 复用），累计值归零
		}
		pct = delta / elapsed.Seconds() * 100 / float64(runtime.NumCPU())
	}

	p.res = procSample{at: now, cpu: cpu, pct: pct}
	return mem, pct
}
