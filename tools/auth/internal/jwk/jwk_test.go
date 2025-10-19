package jwk_test

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/tools/auth/internal/jwk"
)

type publicKey interface {
	Equal(x crypto.PublicKey) bool
}

func TestJWK_PublicKey(t *testing.T) {
	t.Parallel()

	rsaPrivate, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("failed to generate test RSA private key: %v", err)
	}

	scenarios := []struct {
		name        string
		key         *jwk.JWK
		expectError bool
		expectKey   crypto.PublicKey
	}{
		{
			"empty",
			&jwk.JWK{},
			true,
			nil,
		},
		{
			"invalid kty",
			&jwk.JWK{
				Kty: "invalid",
				Alg: "RS256",
				E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaPrivate.E)).Bytes()),
				N:   base64.RawURLEncoding.EncodeToString(rsaPrivate.N.Bytes()),
			},
			true,
			nil,
		},
		{
			"RSA",
			&jwk.JWK{
				Kty: "RSA",
				Alg: "RS256",
				E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaPrivate.E)).Bytes()),
				N:   base64.RawURLEncoding.EncodeToString(rsaPrivate.N.Bytes()),
			},
			false,
			&rsaPrivate.PublicKey,
		},
		{
			"OKP with unsupported curve",
			&jwk.JWK{
				Kty: "OKP",
				Crv: "invalid",
				X:   base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", ed25519.PublicKeySize))),
			},
			true,
			nil,
		},
		{
			"OKP with invalid public key length",
			&jwk.JWK{
				Kty: "OKP",
				Crv: "Ed25519",
				X:   base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", ed25519.PublicKeySize-1))),
			},
			true,
			nil,
		},
		{
			"valid OKP",
			&jwk.JWK{
				Kty: "OKP",
				Crv: "Ed25519",
				X:   base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", ed25519.PublicKeySize))),
			},
			false,
			ed25519.PublicKey([]byte(strings.Repeat("a", ed25519.PublicKeySize))),
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			result, err := s.key.PublicKey()

			hasErr := err != nil
			if hasErr != s.expectError {
				t.Fatalf("Expected hasErr %v, got %v (%v)", s.expectError, hasErr, err)
			}

			if hasErr && result == nil {
				return
			}

			k, ok := result.(publicKey)
			if !ok {
				t.Fatalf("The returned public key %T doesn't satisfy the expected common interface", k)
			}

			if !k.Equal(s.expectKey) {
				t.Fatalf("The returned public key doesn't match with the expected one:\n%v\n%v", k, s.expectKey)
			}
		})
	}
}

func TestFetch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Has("error") {
			res.WriteHeader(http.StatusBadRequest)
		}

		fmt.Fprintf(res, `{
			"keys": [
				{
					"kid": "abc",
					"kty": "OKP",
					"crv": "Ed25519",
					"x":   "test_x"
				},
				{
					"kid": "def",
					"kty": "RSA",
					"alg": "RS256",
					"n":   "test_n",
					"e":   "test_e"
				}
			]
		}`)
	}))
	defer server.Close()

	scenarios := []struct {
		name        string
		kid         string
		expectError bool
		contains    []string
	}{
		{
			"error response",
			"def",
			true,
			nil,
		},
		{
			"non-matching kid",
			"missing",
			true,
			nil,
		},
		{
			"matching kid",
			"def",
			false,
			[]string{
				`"kid":"def"`,
				`"kty":"RSA"`,
				`"alg":"RS256"`,
				`"n":"test_n"`,
				`"e":"test_e"`,
			},
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			url := server.URL
			if s.expectError {
				url += "?error"
			}

			key, err := jwk.Fetch(context.Background(), url, s.kid)

			hasErr := err != nil
			if hasErr != s.expectError {
				t.Fatalf("Expected hasErr %v, got %v (%v)", s.expectError, hasErr, err)
			}

			raw, err := json.Marshal(key)
			if err != nil {
				t.Fatal(err)
			}
			rawStr := string(raw)

			for _, substr := range s.contains {
				if !strings.Contains(rawStr, substr) {
					t.Fatalf("Missing expected substring\n%s\nin\n%s", substr, rawStr)
				}
			}
		})
	}
}
