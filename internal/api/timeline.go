package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type timelineBucket struct {
	Label string `json:"label"`
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count"`
}

func (h *Handlers) handleTimeline(w http.ResponseWriter, r *http.Request) {
	zoom := r.URL.Query().Get("zoom")
	switch zoom {
	case "decade", "year", "month", "week", "day":
	default:
		zoom = "year"
	}

	var whereParts []string
	var args []any
	whereParts = append(whereParts, "captured_at IS NOT NULL")

	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			whereParts = append(whereParts, "captured_at >= ?")
			args = append(args, t.UTC().Format(time.RFC3339))
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24*time.Hour - time.Second)
			whereParts = append(whereParts, "captured_at <= ?")
			args = append(args, end.UTC().Format(time.RFC3339))
		}
	}

	where := "WHERE " + strings.Join(whereParts, " AND ")

	var bucketExpr string
	switch zoom {
	case "decade":
		bucketExpr = "CAST(CAST(strftime('%Y', captured_at) AS INTEGER) / 10 * 10 AS TEXT)"
	case "year":
		bucketExpr = "strftime('%Y', captured_at)"
	case "month":
		bucketExpr = "strftime('%Y-%m', captured_at)"
	case "week":
		bucketExpr = "strftime('%Y-%W', captured_at)"
	case "day":
		bucketExpr = "strftime('%Y-%m-%d', captured_at)"
	}

	query := fmt.Sprintf(
		"SELECT %s AS bucket, COUNT(*) FROM photos %s GROUP BY bucket ORDER BY bucket ASC",
		bucketExpr, where,
	)
	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var buckets []timelineBucket
	var grandTotal int
	for rows.Next() {
		var label string
		var count int
		if err := rows.Scan(&label, &count); err != nil {
			continue
		}
		from, to := timelineBucketRange(zoom, label)
		buckets = append(buckets, timelineBucket{
			Label: label,
			From:  from,
			To:    to,
			Count: count,
		})
		grandTotal += count
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if buckets == nil {
		buckets = []timelineBucket{}
	}

	var undated int
	h.db.QueryRow("SELECT COUNT(*) FROM photos WHERE captured_at IS NULL").Scan(&undated) //nolint:errcheck

	writeJSON(w, http.StatusOK, map[string]any{
		"zoom":    zoom,
		"buckets": buckets,
		"total":   grandTotal,
		"undated": undated,
	})
}

// timelineBucketRange returns ISO date strings for the start and end of a
// calendar bucket, given a zoom level and the label returned by SQLite's
// strftime expression.
func timelineBucketRange(zoom, label string) (string, string) {
	const dateFmt = "2006-01-02"
	var from, to time.Time
	switch zoom {
	case "decade":
		t, err := time.Parse("2006", label+"0") // label is e.g. "2010"
		if err != nil {
			return "", ""
		}
		from = t
		to = t.AddDate(10, 0, 0).Add(-time.Second)
	case "year":
		t, err := time.Parse("2006", label)
		if err != nil {
			return "", ""
		}
		from = t
		to = t.AddDate(1, 0, 0).Add(-time.Second)
	case "month":
		t, err := time.Parse("2006-01", label)
		if err != nil {
			return "", ""
		}
		from = t
		to = t.AddDate(0, 1, 0).Add(-time.Second)
	case "week":
		// SQLite %W: week 00 = days before first Monday; week 01 = starts on first Monday.
		var year, week int
		_, _ = fmt.Sscanf(label, "%d-%d", &year, &week)
		jan1 := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		daysUntilMonday := (int(time.Monday) - int(jan1.Weekday()) + 7) % 7
		firstMonday := jan1.AddDate(0, 0, daysUntilMonday)
		if week == 0 {
			from = jan1
			to = firstMonday.Add(-time.Second)
			if to.Before(from) {
				to = from // degenerate case: Jan 1 is a Monday
			}
		} else {
			from = firstMonday.AddDate(0, 0, (week-1)*7)
			to = from.AddDate(0, 0, 7).Add(-time.Second)
		}
	case "day":
		t, err := time.Parse(dateFmt, label)
		if err != nil {
			return "", ""
		}
		from = t
		to = t.AddDate(0, 0, 1).Add(-time.Second)
	default:
		return "", ""
	}
	return from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339)
}
