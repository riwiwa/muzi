package config

import "testing"

func TestGetDbUrlEscapes(t *testing.T) {
	d := DatabaseConfig{Host: "db", Port: "5432", User: "muzi", Password: "p@ss/w:rd", Name: "muzi"}
	if got, want := d.GetDbUrl(true), "postgres://muzi:p%40ss%2Fw%3Ard@db:5432/muzi"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got, want := d.GetDbUrl(false), "postgres://muzi:p%40ss%2Fw%3Ard@db:5432"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("MUZI_DB_HOST", "db")
	t.Setenv("MUZI_DB_PASSWORD", "secret")
	t.Setenv("MUZI_ALLOW_SIGNUP", "true")
	SetPath("does-not-exist.toml")
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.Database.Host != "db" || c.Database.Password != "secret" || !c.Server.AllowSignup {
		t.Errorf("overrides not applied: %+v", c)
	}
	if c.Database.Port != "5432" {
		t.Errorf("unset fields should keep defaults, got port %q", c.Database.Port)
	}

	t.Setenv("MUZI_ALLOW_SIGNUP", "maybe")
	if _, err := LoadConfig(); err == nil {
		t.Error("expected an error for a non-boolean MUZI_ALLOW_SIGNUP")
	}
}
