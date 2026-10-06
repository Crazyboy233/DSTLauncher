//go:build windows

package server

import (
	"net"
	"os"
	"testing"
)

// 起一个真实的 UDP socket，再让端点表查询把它找出来。
// 这能验证 MIB_UDPROW_OWNER_PID 的解析与字节序换算是否正确——
// 写错的话端口号会是个完全不相干的值，而不是报错。
func TestUDPPortOwnerSelf(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("监听 UDP 失败: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	port := conn.LocalAddr().(*net.UDPAddr).Port
	self := os.Getpid()

	owner, ok := udpPortOwner(port)
	if !ok {
		t.Fatalf("端口 %d 已绑定，却查不到占用者", port)
	}
	if owner != self {
		t.Errorf("占用者应为 %d，实际 %d", self, owner)
	}

	if !portOwnedBy(port, self) {
		t.Error("portOwnedBy 应判定为本进程所有")
	}
	if portOwnedBy(port, self+1) {
		t.Error("不应判定为其它进程所有")
	}
}

// 非法入参不应触发查询，也不应误判
func TestPortOwnedByInvalidArgs(t *testing.T) {
	if portOwnedBy(0, os.Getpid()) {
		t.Error("端口 0 不应判定为已监听")
	}
	if portOwnedBy(11000, 0) {
		t.Error("PID 0 不应判定为已监听")
	}
}
