package finding

import "fmt"

// ValidateSeverity accepts the three stable severities exposed by audit tools.
func ValidateSeverity(value Severity) error {
	switch value {
	case SeverityError, SeverityWarning, SeverityInfo:
		return nil
	default:
		return fmt.Errorf("invalid severity %q (want error, warning, or info)", value)
	}
}

// Filter returns findings at or above min and clamps the visible findings to max.
// Total and CountsBySeverity describe the severity-filtered set before truncation.
func Filter(result AuditResult, min Severity, max int) AuditResult {
	filtered := make([]Finding, 0, len(result.Findings))
	counts := make(map[Severity]int)
	minimum := severityRank(min)
	for _, item := range result.Findings {
		if severityRank(item.Severity) > minimum {
			continue
		}
		filtered = append(filtered, item)
		counts[item.Severity]++
	}

	result.Total = len(filtered)
	result.CountsBySeverity = counts
	result.Truncated = len(filtered) > max
	if result.Truncated {
		filtered = filtered[:max]
	}
	result.Findings = filtered
	return result
}

func severityRank(value Severity) int {
	switch value {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	default:
		return 3
	}
}
