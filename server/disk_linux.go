package main

import "syscall"

func diskHasReserve(dir string) bool {
	var stat syscall.Statfs_t
	if syscall.Statfs(dir, &stat) != nil {
		return false
	}
	return stat.Bavail*uint64(stat.Bsize) > 64*1024*1024
}
