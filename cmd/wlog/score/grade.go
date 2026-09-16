package score

// Grade turns a 0-100 score into a letter, so a report reads at a glance.
//
//	A 90-100, B 80-89, C 70-79, D 60-69, F below 60.
func Grade(percent int) string {
	switch {
	case percent >= 90:
		return "A"
	case percent >= 80:
		return "B"
	case percent >= 70:
		return "C"
	case percent >= 60:
		return "D"
	default:
		return "F"
	}
}

// classWeight is how much one entry point's class counts in the total.
func classWeight(class string) float64 {
	switch class {
	case "sensitive":
		return 2
	case "write":
		return 1.5
	default:
		return 1
	}
}
