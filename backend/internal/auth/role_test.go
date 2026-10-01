package auth

import (
	"testing"
	"time"
)

func TestAdminRole(t *testing.T) {
	a := New([]byte("secret"), []string{"alice@example.com", "bob@example.com", "root@example.com"}, 0)

	if a.IsAdmin("alice@example.com") {
		t.Error("IsAdmin must be false before SetAdmins")
	}
	if a.Role("alice@example.com") != RoleUser {
		t.Errorf("Role before SetAdmins = %q, want %q", a.Role("alice@example.com"), RoleUser)
	}

	a.SetAdmins([]string{"ALICE@Example.com", "bob@example.com"})
	if !a.IsAdmin("alice@example.com") {
		t.Error("IsAdmin must be true after SetAdmins (normalization)")
	}
	if !a.IsAdmin("bob@example.com") {
		t.Error("IsAdmin(bob) = false, want true")
	}
	if a.IsAdmin("carol@example.com") {
		t.Error("IsAdmin(carol) = true, want false")
	}
	if a.IsSuperAdmin("alice@example.com") {
		t.Error("a database admin must not be promoted to superadmin")
	}

	a.SetAdmins(nil)
	if a.IsAdmin("alice@example.com") {
		t.Error("admin role not cleared by SetAdmins(nil)")
	}
}

func TestSuperAdminIsImplicitlyAllowed(t *testing.T) {
	// The superadmin sits above the allowlist: if it could be locked out by
	// ALLOWED_EMAILS, the last account able to fix the allowlist would be gone.
	a := New([]byte("secret"), []string{"alice@example.com"}, time.Hour)
	if a.Allowed("root@example.com") {
		t.Error("root must not be allowed before SetSuperAdmin")
	}

	a.SetSuperAdmin("root@example.com")
	if !a.Allowed("root@example.com") {
		t.Error("superadmin must be implicitly allowlisted")
	}
	if !a.Allowed("ROOT@example.com") {
		t.Error("superadmin allowance must be case-insensitive")
	}
	if a.Allowed("bob@example.com") {
		t.Error("unrelated email must not inherit the superadmin allowance")
	}

	// A session token issued before the allowlist changes still verifies.
	token, err := a.Issue("root@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := a.Verify(token); err != nil {
		t.Fatalf("superadmin session must verify: %v", err)
	}

	// Non-allowlisted non-superadmins are still rejected.
	bad, err := a.Issue("bob@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := a.Verify(bad); err == nil {
		t.Error("non-allowlisted session must not verify")
	}
}

func TestAllowlistSnapshot(t *testing.T) {
	a := New([]byte("secret"), []string{"a@x.com", "b@x.com"}, time.Hour)
	got := a.Allowlist()
	if len(got) != 2 {
		t.Fatalf("Allowlist() = %v, want 2 entries", got)
	}
	a.SetAllowed([]string{"c@x.com"})
	if got := a.Allowlist(); len(got) != 1 || got[0] != "c@x.com" {
		t.Fatalf("Allowlist() after SetAllowed = %v", got)
	}
}

func TestSuperAdminOutranksAdmins(t *testing.T) {
	a := New([]byte("secret"), []string{"root@example.com", "alice@example.com"}, 0)
	a.SetAdmins([]string{"alice@example.com"})
	a.SetSuperAdmin(" ROOT@Example.com ")

	if !a.IsSuperAdmin("root@example.com") {
		t.Fatal("IsSuperAdmin must normalize the configured email")
	}
	if a.SuperAdmin() != "root@example.com" {
		t.Errorf("SuperAdmin() = %q", a.SuperAdmin())
	}
	// The superadmin is not required to be in the database admin list.
	if a.Role("root@example.com") != RoleSuperAdmin {
		t.Errorf("Role(superadmin) = %q, want %q", a.Role("root@example.com"), RoleSuperAdmin)
	}
	if !a.IsAdmin("root@example.com") {
		t.Error("IsAdmin(superadmin) must be true so superadmin can reach admin pages")
	}
	if a.IsSuperAdmin("alice@example.com") {
		t.Error("regular admin must not be superadmin")
	}
	if a.Role("alice@example.com") != RoleAdmin {
		t.Errorf("Role(alice) = %q, want %q", a.Role("alice@example.com"), RoleAdmin)
	}

	a.SetSuperAdmin("")
	if a.IsSuperAdmin("root@example.com") {
		t.Error("SetSuperAdmin(\"\") must clear the superadmin")
	}
	if a.Role("root@example.com") != RoleUser {
		t.Errorf("after clearing, Role = %q, want %q", a.Role("root@example.com"), RoleUser)
	}
}
