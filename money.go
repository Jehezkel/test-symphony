package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var errInvalidMoney = errors.New("invalid money amount")

// parseMinorUnits maps a decimal amount with at most two fractional digits to
// integer minor units. It deliberately never passes through a float.
func parseMinorUnits(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errInvalidMoney
	}
	sign := int64(1)
	if value[0] == '-' || value[0] == '+' {
		if value[0] == '-' {
			sign = -1
		}
		value = value[1:]
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && len(parts[1]) > 2) {
		return 0, errInvalidMoney
	}
	for len(parts) == 2 && len(parts[1]) < 2 {
		parts[1] += "0"
	}
	fraction := "00"
	if len(parts) == 2 {
		fraction = parts[1]
	}
	major, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, errInvalidMoney
	}
	minor, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil || minor < 0 {
		return 0, errInvalidMoney
	}
	if major > (int64(^uint64(0)>>1)-minor)/100 {
		return 0, errInvalidMoney
	}
	return sign * (major*100 + minor), nil
}

func formatMinorUnits(value int64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	return fmt.Sprintf("%s%d.%02d", sign, value/100, value%100)
}
