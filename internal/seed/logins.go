package seed

import (
	"slices"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// DemoPassword is the password of every demo account. Like all demo data it
// is fictional, and it is published on the demo's login page.
const DemoPassword = "demo-password"

// demoPasswordHash is DemoPassword hashed with the cheap demo parameters of
// package auth/password, so the WebAssembly demo does not hash at startup.
// A server upgrades it to its own parameters at the first login.
const demoPasswordHash = "$argon2id$v=19$m=1024,t=1,p=1$ZGVtby1zZWVkLXNhbHQtMQ$sxSynfRZl9y9AigRtRVAVxaqp24lgJlVioaAAiofZSs" //nolint:gosec // G101: fictional, published

// Login is a demo account to sign in with.
type Login struct {
	Email      string
	Password   string
	Role       domain.Role
	GivenName  string
	FamilyName string
}

// DemoLogins returns the first seeded account of each role, from least to
// most privileged role.
func DemoLogins() []Login {
	d := Demo()

	var logins []Login

	for _, role := range domain.Roles() {
		i := slices.IndexFunc(d.Accounts, func(a domain.Account) bool { return a.Role == role })
		if i < 0 {
			continue // guests have no account
		}

		acc := d.Accounts[i]
		p := d.Persons[slices.IndexFunc(d.Persons, func(p domain.Person) bool { return p.ID == acc.PersonID })]
		logins = append(logins, Login{
			Email: acc.Email, Password: DemoPassword, Role: role, GivenName: p.GivenName, FamilyName: p.FamilyName,
		})
	}

	return logins
}
