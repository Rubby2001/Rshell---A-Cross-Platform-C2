package service

import (
	"Rshell/pkg/logger"
	"embed"
	"strings"
)

//go:embed server/*
var EmbeddedFiles embed.FS

//go:embed stageshellcode/*
var EmbeddedStager embed.FS

// FindBinary 查找匹配的二进制文件
func FindBinary(listenerType, osType, archType string) string {
	entries, err := EmbeddedFiles.ReadDir("server/" + listenerType)
	if err != nil {
		logger.Error("读取嵌入目录失败: %v\n", err)
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "r_"+osType+"_"+archType || name == "r_"+osType+"_"+archType+".exe" {
			return name
		}
	}
	return ""
}

// PadRight 填充字符串到指定长度
func PadRight(str string, length int) string {
	if len(str) >= length {
		return str
	}
	return str + strings.Repeat(" ", length-len(str))
}
