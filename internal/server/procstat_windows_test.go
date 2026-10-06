//go:build windows

package server

import (
	"os"
	"testing"
	"time"
)

// 对自身进程采样是最可靠的自检：一定能打开、一定有非零工作集。
// 结构体布局写错（例如 PROCESS_MEMORY_COUNTERS 的对齐不对）会在这里暴露成 0 或超大值。
func TestQueryProcessSelf(t *testing.T) {
	pid := os.Getpid()

	cpu1, mem1, err := queryProcess(pid)
	if err != nil {
		t.Fatalf("采样失败: %v", err)
	}
	if mem1 == 0 {
		t.Error("内存占用为 0，工作集应该非零")
	}
	// 上限取 64GB：明显超过说明结构体布局错位，读到了垃圾字段
	if mem1 > 64<<30 {
		t.Errorf("内存占用异常: %d 字节", mem1)
	}
	if cpu1 <= 0 {
		t.Error("累计 CPU 时间应大于 0")
	}

	// 累计 CPU 时间必须单调不减
	time.Sleep(60 * time.Millisecond)
	cpu2, _, err := queryProcess(pid)
	if err != nil {
		t.Fatalf("二次采样失败: %v", err)
	}
	if cpu2 < cpu1 {
		t.Errorf("累计 CPU 时间倒退了: %v -> %v", cpu1, cpu2)
	}
}

// 不存在的 PID 应报错而不是返回零值
func TestQueryProcessInvalidPID(t *testing.T) {
	cpu, mem, err := queryProcess(0x7fffffff)
	if err == nil {
		t.Errorf("预期报错，实际返回 cpu=%v mem=%d", cpu, mem)
	}
}

// 采样间隔过短时应复用上次结果，避免数值抖动
func TestProcResourceThrottlesShortInterval(t *testing.T) {
	m := &Manager{}
	p := &process{pid: os.Getpid(), state: StateRunning}

	_, first := m.procResource(p) // 首次无基准 -> 0
	if first != 0 {
		t.Errorf("首次采样占用率应为 0，实际 %v", first)
	}

	// 立刻再采一次：间隔远小于 minSampleInterval，应复用上次的 0
	_, second := m.procResource(p)
	if second != 0 {
		t.Errorf("间隔过短时应复用上次结果（0），实际 %v", second)
	}

	// 等过最小间隔后再采，应该已经算出真实值（可能仍是 0，但 p.res.at 必须推进）
	p.mu.RLock()
	before := p.res.at
	p.mu.RUnlock()

	time.Sleep(minSampleInterval + 50*time.Millisecond)
	m.procResource(p)

	p.mu.RLock()
	after := p.res.at
	p.mu.RUnlock()
	if !after.After(before) {
		t.Errorf("超出最小间隔后应重新采样，时间戳未推进: %v -> %v", before, after)
	}
}
