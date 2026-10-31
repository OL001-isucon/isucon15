package isureal

import (
	"net/http"
	"testing"
	"time"
)

func TestAbuseDetectorBansAtThreshold(t *testing.T) {
	d := newAbuseDetector(3, 5*time.Second)
	base := time.Unix(1793000000, 0)
	if d.observe("a", base) || d.observe("a", base.Add(time.Second)) {
		t.Fatal("banned before the threshold")
	}
	if !d.observe("a", base.Add(2*time.Second)) {
		t.Fatal("not banned at the threshold")
	}
	if d.observe("a", base.Add(3*time.Second)) {
		t.Fatal("count was not reset after a ban")
	}
}

func TestAbuseDetectorIgnoresOldFailures(t *testing.T) {
	d := newAbuseDetector(2, 5*time.Second)
	base := time.Unix(1793000000, 0)
	d.observe("a", base)
	if d.observe("a", base.Add(5*time.Second)) {
		t.Fatal("a failure outside the window was counted")
	}
	if !d.observe("a", base.Add(6*time.Second)) {
		t.Fatal("failures inside the window were not counted")
	}
}

func TestAbuseDetectorSeparatesIPs(t *testing.T) {
	d := newAbuseDetector(2, 5*time.Second)
	base := time.Unix(1793000000, 0)
	d.observe("a", base)
	if d.observe("b", base) {
		t.Fatal("failures of another IP were counted")
	}
}

func TestAbuseGuardConditions(t *testing.T) {
	g := NewAbuseGuard(nil)
	cases := []struct {
		name   string
		action string
		status int
		abuse  string
		want   *abuseDetector
	}{
		{"login failure", "login", http.StatusUnauthorized, "", g.login},
		{"login success", "login", http.StatusOK, "", nil},
		{"invalid image", "put_profile_image", http.StatusBadRequest, "invalid_image", g.image},
		{"too large image", "put_profile_image", http.StatusBadRequest, "", nil},
		{"invalid signature", "get_image", http.StatusForbidden, "invalid_signature", g.signature},
		{"deleted image", "get_image", http.StatusForbidden, "", nil},
		{"other action", "get_me", http.StatusUnauthorized, "", nil},
	}
	for _, c := range cases {
		if got := g.detectorFor(c.action, c.status, c.abuse); got != c.want {
			t.Errorf("%s: wrong detector", c.name)
		}
	}
}
