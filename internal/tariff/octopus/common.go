package octopus

import "strings"

func productName(code string) string {
	code = strings.ToUpper(code)
	if strings.Contains(code, "AGILE") {
		if strings.Contains(code, "OUTGOING") {
			return "Octopus Agile Outgoing"
		}
		return "Octopus Agile"
	}
	if strings.Contains(code, "GO") {
		return "Octopus Go"
	}
	if strings.Contains(code, "OUTGOING") {
		return "Octopus Outgoing"
	}
	return "Octopus " + code
}
