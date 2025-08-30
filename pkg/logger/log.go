package logger

import (
	"fmt"
	"time"

	"github.com/fatih/color"
)

var (
	infoColor  = color.New(color.FgGreen).SprintFunc()
	warnColor  = color.New(color.FgYellow).SprintFunc()
	errorColor = color.New(color.FgRed).SprintFunc()
	timeColor  = color.New(color.FgHiBlack).SprintFunc()
)

func timestamp() string {
	return timeColor(time.Now().Format("2006-01-02 15:04:05"))
}

func Info(format string, args ...interface{}) {
	prefix := infoColor("[INFO]")
	fmt.Printf("%s %s %s\n", timestamp(), prefix, fmt.Sprintf(format, args...))
}

func Warn(format string, args ...interface{}) {
	prefix := warnColor("[WARN]")
	fmt.Printf("%s %s %s\n", timestamp(), prefix, fmt.Sprintf(format, args...))
}

func Error(format string, args ...interface{}) {
	prefix := errorColor("[ERROR]")
	fmt.Printf("%s %s %s\n", timestamp(), prefix, fmt.Sprintf(format, args...))
}
