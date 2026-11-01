package team

import "testing"

func TestProductionRiskWord(t *testing.T) {
	for _, goal := range []string{"Write reproduction steps for the UI bug", "Improve reproduction of a local failure"} {
		if risk, reasons := Assess(goal, nil); risk == 4 {
			t.Fatalf("ordinary reproduction was blocked: %s: %v", goal, reasons)
		}
	}
	for _, goal := range []string{"Change production", "PRODUCTION deployment", "edit /production/config", "production_db", "生产发布", "git push", "read credentials", "terraform apply"} {
		if risk, _ := Assess(goal, nil); risk != 4 {
			t.Fatalf("high-risk instruction was not blocked: %s", goal)
		}
	}
}
