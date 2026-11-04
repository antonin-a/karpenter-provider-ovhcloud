package client

import "testing"

func TestCredentialsValidate(t *testing.T) {
	tests := []struct {
		name    string
		creds   Credentials
		wantErr bool
	}{
		{"application key trio", Credentials{Endpoint: "ovh-eu", ApplicationKey: "ak", ApplicationSecret: "as", ConsumerKey: "ck"}, false},
		{"oauth2 service account", Credentials{Endpoint: "ovh-us", ClientID: "US.id", ClientSecret: "secret"}, false},
		{"nothing set", Credentials{Endpoint: "ovh-eu"}, true},
		{"client id without secret", Credentials{Endpoint: "ovh-us", ClientID: "US.id"}, true},
		{"application key without consumer key", Credentials{Endpoint: "ovh-eu", ApplicationKey: "ak", ApplicationSecret: "as"}, true},
		{"both kinds at once", Credentials{Endpoint: "ovh-eu", ApplicationKey: "ak", ApplicationSecret: "as", ConsumerKey: "ck", ClientID: "EU.id", ClientSecret: "secret"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.creds.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewOVHClientWithServiceAccountUsesOAuth2(t *testing.T) {
	c, err := NewOVHClient(&Credentials{Endpoint: "ovh-us", ClientID: "US.id", ClientSecret: "secret"}, "project", "kube", "US-EAST-VA-1")
	if err != nil {
		t.Fatalf("NewOVHClient() error = %v", err)
	}
	if c.client.ClientID != "US.id" || c.client.ClientSecret != "secret" {
		t.Errorf("client not configured for OAuth2: ClientID=%q", c.client.ClientID)
	}
	if c.client.AppKey != "" || c.client.ConsumerKey != "" {
		t.Errorf("OAuth2 client must not carry application keys: AppKey=%q", c.client.AppKey)
	}
}

func TestNewOVHClientWithApplicationKeys(t *testing.T) {
	c, err := NewOVHClient(&Credentials{Endpoint: "ovh-eu", ApplicationKey: "ak", ApplicationSecret: "as", ConsumerKey: "ck"}, "project", "kube", "GRA7")
	if err != nil {
		t.Fatalf("NewOVHClient() error = %v", err)
	}
	if c.client.AppKey != "ak" || c.client.ClientID != "" {
		t.Errorf("expected application-key client, got AppKey=%q ClientID=%q", c.client.AppKey, c.client.ClientID)
	}
}
