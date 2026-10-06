//go:build windows

package server

import (
	"syscall"
	"unsafe"
)

// 只读地查询「某个 UDP 端口是否正被某个进程监听」。
//
// 为什么不用更简单的 TCP 连接测试：DST 的游戏端口是 **UDP**，
// UDP 没有连接建立的过程，Dial 一定"成功"，探测不出任何信息。
//
// 为什么也不自行绑定端口来试探：那会短暂抢占该端口，
// 万一服务器正在这一刻绑定，就会把它的启动搞失败——探测反而成了干扰源。
//
// 所以查系统的端点表（iphlpapi!GetExtendedUdpTable），纯读取、零副作用。

const (
	afInet           = 2 // AF_INET
	udpTableOwnerPID = 1 // UDP_TABLE_OWNER_PID
)

var (
	iphlpapi           = syscall.NewLazyDLL("iphlpapi.dll")
	procGetExtendedUDP = iphlpapi.NewProc("GetExtendedUdpTable")
)

// MIB_UDPROW_OWNER_PID
type mibUDPRowOwnerPID struct {
	localAddr uint32
	localPort uint32 // 网络字节序
	owningPID uint32
}

// udpPortOwner 返回占用指定 UDP 端口的进程 PID。
func udpPortOwner(port int) (int, bool) {
	var size uint32

	// 第一次调用只为问出所需缓冲区大小：预期失败并回填 size
	procGetExtendedUDP.Call(0, uintptr(unsafe.Pointer(&size)), 0, afInet, udpTableOwnerPID, 0)
	if size == 0 {
		return 0, false
	}

	buf := make([]byte, size)
	r, _, _ := procGetExtendedUDP.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0, afInet, udpTableOwnerPID, 0)
	if r != 0 { // NO_ERROR == 0
		return 0, false
	}

	// 表头是 DWORD 条目数，随后是定长行数组
	count := *(*uint32)(unsafe.Pointer(&buf[0]))
	rowSize := int(unsafe.Sizeof(mibUDPRowOwnerPID{}))

	for i := 0; i < int(count); i++ {
		off := 4 + i*rowSize
		if off+rowSize > len(buf) {
			break
		}
		row := (*mibUDPRowOwnerPID)(unsafe.Pointer(&buf[off]))
		// dwLocalPort 是网络字节序（只用到低 16 位）
		p := int(row.localPort&0xff)<<8 | int(row.localPort>>8)&0xff
		if p == port {
			return int(row.owningPID), true
		}
	}
	return 0, false
}

// portOwnedBy 判断端口是否正被指定进程监听。
func portOwnedBy(port, pid int) bool {
	if port <= 0 || pid <= 0 {
		return false
	}
	owner, ok := udpPortOwner(port)
	return ok && owner == pid
}
