package googleapi

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxReadRetries = 6
const maxReadRetryElapsed = 2 * time.Minute

func retryableHTTP(err *HTTPError) bool {
	return err.Status == 429 || err.Status >= 500 || err.Status == 403 && err.hasQuotaReason()
}

// Gmail quota windows outlast subsecond retries. Allow a read to span a minute
// with exponential backoff and jitter, while retaining a finite request budget.
// Writes deliberately do not use this policy: a missing receipt is ambiguous.
func readRetryDelay(retry int, retryAfter string, now time.Time) time.Duration {
	delay := (time.Second << retry) + time.Duration(rand.Int64N(int64(time.Second)))
	retryAfter = strings.TrimSpace(retryAfter)
	seconds, err := strconv.ParseUint(retryAfter, 10, 64)
	if err == nil || errors.Is(err, strconv.ErrRange) {
		if seconds > uint64(maxReadRetryElapsed/time.Second) {
			return maxReadRetryElapsed + time.Second
		}
		return max(delay, time.Duration(seconds)*time.Second)
	}
	if date, err := http.ParseTime(retryAfter); err == nil {
		return max(delay, date.Sub(now))
	}
	return delay
}
