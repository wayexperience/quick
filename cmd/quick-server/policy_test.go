package main

import "testing"

func TestCanWrite(t *testing.T) {
	const me, other = "me@x", "other@x"
	owned := policy{CreatedBy: other}                           // sito creato da un altro
	mine := policy{CreatedBy: me}                               // sito creato da me
	locked := policy{CreatedBy: me, Locked: true, Owner: other} // bloccato da altri

	cases := []struct {
		name      string
		ownership string
		p         policy
		action    string
		want      bool
	}{
		{"free deploy altrui ok", "free", owned, actDeploy, true},
		{"free delete altrui ok", "free", owned, actDelete, true},
		{"shared deploy altrui ok", "shared", owned, actDeploy, true},
		{"shared delete altrui no", "shared", owned, actDelete, false},
		{"shared policy altrui no", "shared", owned, actPolicy, false},
		{"shared delete mio ok", "shared", mine, actDelete, true},
		{"owned deploy altrui no", "owned", owned, actDeploy, false},
		{"owned deploy mio ok", "owned", mine, actDeploy, true},
		{"owned deploy senza creatore ok", "owned", policy{}, actDeploy, true},
		{"lock altrui blocca sempre", "free", locked, actDeploy, false},
	}
	for _, c := range cases {
		s := &server{ownership: c.ownership}
		got, _ := s.canWrite(c.p, me, c.action)
		if got != c.want {
			t.Errorf("%s: canWrite=%v, voluto %v", c.name, got, c.want)
		}
	}
}
