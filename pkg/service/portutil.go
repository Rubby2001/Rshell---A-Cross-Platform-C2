package service

import (
	"fmt"
	"net"
)

// IsPortInUse 检测指定端口是否被占用
func IsPortInUse(port string) (bool, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		if opErr, ok := err.(*net.OpError); ok {
			if opErr.Err.Error() == "bind: address already in use" ||
				opErr.Err.Error() == "listen tcp :"+port+": bind: Only one usage of each socket address (protocol/network address/port) is normally permitted." {
				return true, nil
			}
		}
		return false, err
	}
	_ = listener.Close()
	return false, nil
}

// IsPortAvailable 检查端口是否可用
func IsPortAvailable(address string) bool {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}
