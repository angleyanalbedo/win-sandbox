package main

import (
	"os"

	"github.com/angleyanalbedo/win-sandbox/cmd"
	"github.com/sirupsen/logrus"
)

func main() {
	// 配置日志
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "15:04:05",
	})

	if err := cmd.Execute(); err != nil {
		logrus.WithError(err).Fatal("执行失败")
		os.Exit(1)
	}
}
