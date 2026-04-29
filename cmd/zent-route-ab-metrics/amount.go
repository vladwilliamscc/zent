package main

import "fmt"

const atomsPerCoin int64 = 100000000

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

func formatShare(part, total int64) string {
	if total <= 0 {
		return "0.00000000"
	}
	scaled := part * atomsPerCoin / total
	return formatAtoms(scaled)
}
