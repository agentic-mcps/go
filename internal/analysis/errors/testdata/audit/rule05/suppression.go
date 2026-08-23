package rule05

import "log/slog"

func Suppressed(err error) error {
	if err != nil {
		slog.Error("request failed", "error", err)
		return err
	}
	return nil
}
