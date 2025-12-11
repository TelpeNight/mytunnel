package dial

import "log/slog"

var localLogger *slog.Logger

func logger() *slog.Logger {
	if localLogger == nil {
		return slog.Default()
	}
	return localLogger
}
