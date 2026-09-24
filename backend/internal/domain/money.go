package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Money is an amount in the trip currency, counted in hundredths (cents). The
// database keeps it as numeric(14,2), so the largest value is 999 999 999 999.99,
// which fits an int64 with room to spare and never goes through a float.
type Money int64

// maxMoney is the largest amount numeric(14,2) can hold, in hundredths.
const maxMoney Money = 99_999_999_999_999

// ParseMoney - reads a decimal amount such as "1240.5" or "85".
//
// The API carries amounts as decimal strings rather than JSON numbers, so no
// client ever rounds one through binary floating point. Negative amounts and
// more than two fractional digits are refused rather than rounded.
//
// Arguments:
//   - field: the input field name, used in the validation error.
//   - value: the decimal string.
//
// Returns:
//   - the amount in hundredths.
//   - a *ValidationError when the string is not a non-negative amount.
func ParseMoney(field, value string) (Money, error) {
	invalid := NewValidationError(field, "invalid_amount", "must be a non-negative decimal with at most two fractional digits")

	whole, fraction, hasFraction := strings.Cut(strings.TrimSpace(value), ".")
	if whole == "" || len(whole) > 12 || (hasFraction && (fraction == "" || len(fraction) > 2)) {
		return 0, invalid
	}
	for _, part := range []string{whole, fraction} {
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0, invalid
			}
		}
	}

	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, invalid
	}
	cents := int64(0)
	if fraction != "" {
		cents, _ = strconv.ParseInt((fraction + "0")[:2], 10, 64)
	}
	amount := Money(units*100 + cents)
	if amount > maxMoney {
		return 0, invalid
	}
	return amount, nil
}

// String - formats the amount as a decimal with exactly two fractional digits.
//
// Returns:
//   - the decimal string, such as "1240.50".
func (m Money) String() string {
	sign := ""
	value := int64(m)
	if value < 0 {
		sign, value = "-", -value
	}
	return fmt.Sprintf("%s%d.%02d", sign, value/100, value%100)
}
