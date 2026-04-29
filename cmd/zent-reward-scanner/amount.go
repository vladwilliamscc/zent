package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const atomsPerCoin int64 = 100000000

func parseRewardAtoms(raw json.RawMessage) (int64, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, err
	}
	valRaw, ok := value["Val"]
	if !ok {
		return 0, fmt.Errorf("missing value.Val")
	}

	var s string
	if err := json.Unmarshal(valRaw, &s); err != nil {
		s = string(valRaw)
	}
	return parseDecimalAtoms(s)
}

func parseDecimalAtoms(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty decimal")
	}

	sign := int64(1)
	if strings.HasPrefix(s, "-") {
		sign = -1
		s = s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}

	mantissa := s
	exp := int64(0)
	if idx := strings.IndexAny(s, "eE"); idx >= 0 {
		if strings.IndexAny(s[idx+1:], "eE") >= 0 {
			return 0, fmt.Errorf("invalid exponent decimal %q", s)
		}
		if idx == len(s)-1 {
			return 0, fmt.Errorf("missing exponent in decimal %q", s)
		}
		var err error
		exp, err = strconv.ParseInt(s[idx+1:], 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid exponent in decimal %q: %w", s, err)
		}
		mantissa = s[:idx]
	}

	parts := strings.Split(mantissa, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid decimal %q", mantissa)
	}

	wholePart := parts[0]
	fracPart := ""
	if len(parts) == 2 {
		fracPart = parts[1]
	}

	digits := wholePart + fracPart
	if digits == "" {
		return 0, fmt.Errorf("missing digits in decimal %q", s)
	}
	for _, ch := range digits {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("non-numeric decimal %q", s)
		}
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return 0, nil
	}

	scale := int64(len(fracPart)) - exp
	value := new(big.Int)
	if _, ok := value.SetString(digits, 10); !ok {
		return 0, fmt.Errorf("non-numeric decimal %q", s)
	}

	switch {
	case scale > 8:
		truncate := scale - 8
		if truncate >= int64(len(digits)) {
			return 0, fmt.Errorf("decimal %q has more than 8 non-zero fractional digits", s)
		}
		cut := len(digits) - int(truncate)
		if strings.Trim(digits[cut:], "0") != "" {
			return 0, fmt.Errorf("decimal %q has more than 8 non-zero fractional digits", s)
		}
		digits = digits[:cut]
		value.SetString(digits, 10)
	case scale < 8:
		value.Mul(value, pow10(8-scale))
	}

	if sign < 0 {
		value.Neg(value)
	}
	if !value.IsInt64() {
		return 0, fmt.Errorf("decimal %q overflows int64 atoms", s)
	}
	return value.Int64(), nil
}

func formatAtoms(atoms int64) string {
	sign := ""
	if atoms < 0 {
		sign = "-"
		atoms = -atoms
	}
	whole := atoms / atomsPerCoin
	frac := atoms % atomsPerCoin
	return fmt.Sprintf("%s%d.%08d", sign, whole, frac)
}

func pow10(exp int64) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(exp), nil)
}
