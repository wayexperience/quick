package main

import "testing"

func TestCanWrite(t *testing.T) {
	const me, other = "me@x", "other@x"
	owned := policy{CreatedBy: other}                           // created by someone else
	mine := policy{CreatedBy: me}                               // created by me
	locked := policy{CreatedBy: me, Locked: true, Owner: other} // locked by someone else

	cases := []struct {
		name      string
		ownership string
		p         policy
		action    string
		want      bool
	}{
		{"free deploy others ok", "free", owned, actDeploy, true},
		{"free delete others ok", "free", owned, actDelete, true},
		{"shared deploy others ok", "shared", owned, actDeploy, true},
		{"shared delete others no", "shared", owned, actDelete, false},
		{"shared policy others no", "shared", owned, actPolicy, false},
		{"shared delete mine ok", "shared", mine, actDelete, true},
		{"owned deploy others no", "owned", owned, actDeploy, false},
		{"owned deploy mine ok", "owned", mine, actDeploy, true},
		{"owned deploy without creator ok", "owned", policy{}, actDeploy, true},
		{"others' lock always blocks", "free", locked, actDeploy, false},
	}
	for _, c := range cases {
		s := &server{ownership: c.ownership}
		got, _ := s.canWrite(c.p, me, c.action)
		if got != c.want {
			t.Errorf("%s: canWrite=%v, want %v", c.name, got, c.want)
		}
	}
}
