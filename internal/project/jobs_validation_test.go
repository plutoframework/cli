package project

import "testing"

func TestJobKindUniqueness(t *testing.T) {
	jobs := []Job{
		{Group: "billing", Action: "Send", Kind: "billing.send"},
		{Group: "billing", Action: "Retry", Kind: "billing.retry"},
	}
	if err := validateJobs(jobs); err != nil {
		t.Fatal(err)
	}

	jobs[1].Kind = jobs[0].Kind
	if err := validateJobs(jobs); err == nil {
		t.Fatal(
			"distinct job identities accepted duplicate persisted delivery " +
				"kind",
		)
	}
}
