package controllers

import (
	"fmt"
	"time"
)

func past(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	for _, layout := range []string{time.RFC3339, time.DateTime} {
		var at time.Time
		var err error
		if layout == time.DateTime {
			at, err = time.ParseInLocation(layout, value, time.Local)
		} else {
			at, err = time.Parse(layout, value)
		}
		if err == nil {
			return !time.Now().Before(at), nil
		}
	}
	return false, fmt.Errorf("invalid expiry time %q", value)
}
