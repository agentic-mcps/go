package rule04

import "log/slog"

func Bad(err error) error {
	if err != nil { // VIOLATION: errors-04
		slog.Error("request failed", "error", err)
		return err
	}
	return nil
}
