//go:build !windows

// Package main 非 Windows 平台：单实例保护为空操作（开发调试用，CI 通常只跑 Windows）。
package main

import "errors"

// SingleInstance 占位，避免 main.go 在 !windows 平台编译失败。
type SingleInstance struct{}

func AcquireSingleInstance() (*SingleInstance, error) {
	return &SingleInstance{}, nil
}

func (s *SingleInstance) Release() {}

// FileChannel 非 Windows 无 IPC：返回 nil。
func (s *SingleInstance) FileChannel() <-chan string { return nil }

// StartListener 非 Windows 空操作。
func (s *SingleInstance) StartListener() error { return nil }

// StopListener 非 Windows 空操作。
func (s *SingleInstance) StopListener() {}

// SendPathToRunningInstance 非 Windows 无主实例可交接。
func SendPathToRunningInstance(path string) error {
	return errors.New("workbaby: single-instance ipc not supported on this platform")
}

var errInstanceAlreadyRunning = errors.New("workbaby: single-instance not supported on this platform")

func ErrInstanceAlreadyRunning() error { return errInstanceAlreadyRunning }
