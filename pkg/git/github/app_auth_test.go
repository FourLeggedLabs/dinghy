/*
* Copyright 2026 Four Legged Labs
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*    http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */

package github

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/fourleggedlabs/dinghy/pkg/settings/global"
)

// testKeyPEM is a throwaway RSA key generated for tests only.
const testKeyPEM = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDKLcEUyv3jEYxW
+cNknBbZzc5SlSRwE9pjfZzCq4WKKRg1msv9Pa23Sf+ovCFJZZOdeGvZAgivXZJp
Lg6DKIV8WBmgcgTeVpV90EyWbB+sy6h4Ek1RIbfMuz/V0p+D52r2+1qN5zewnhWR
Jrzbf4/5627qZYjIYT7ox6k2SkB5k/NFCf/ROWQNCval5+x1rKfbYEmlLFTCCxUl
sOsY0H33bugOSvfUhDDAIMtygqlBJmYARMVPvrlugeclznhyeq/QA5fulwpcHI9x
/fb+BkS1GqPn8FoFPfS34SuwV4O5GedSBykVBGpKa6b4EKzxgzDKTKVoDYaKbia9
uWlT1CmrAgMBAAECggEAMuIdDAyipTlPZrxpbrLSFXL0kFg02XhFqHB+uYfNjh4V
l7gjytJxHAYlr+PZRM2pvyIFkpIueWRFau9Ke7wBDHBn0reffg2whf+cpucDecuv
1LhWeSrRRVeDE191Ag+GHi7YdYpRu8OtjeB/+4Y5SB72xUtUh4nh+Vf2wFEjtPUS
ysL9+lRXM1Om4N2Z6HTfGhu74Dd3x/zNnfv1q3hXGAUI1CF3g39laNyKDi8gfVvg
UUQg3uI+S1jozny969RDbdm93ZLMVg1XQ/yCzWXtk3Rb0mSnw8nTf4CglOVCMGYz
MNy6iPf/5QahAgWZZmSW5bZEDkNjKMLS0ISVRwuOwQKBgQDuY1JSOZZexYua1TcY
zha1jhr9HqWn3E8nl/r8LsYfSFiMHSmq8HcNmPyG0R5HNS1uv6yPbe2UYtzeRcjM
30wfiYuPqpg8Td8Zc93zxphAH50GDKQVKHW9gJwvR4cgDWIMpj24rgvxY/2eR2CG
N2by0G7HLfkh4Q/nEN1ck8cb+wKBgQDZHZmeTKKnyYR6Mmb3gbvi2+TSaJy3BoFD
9gxL3JF8MutCn/uXIJluZMqDOufNeWNxidiGyrRpt0FDvUM4J0dIwPDZvrFDsKea
Sm3CbyNZBtMV2gyB0jv7GUgG2MiUwLowTF3D4LUZPrwqRF/i/o6kMPp99WyfU/Ek
5YUTDrKKEQKBgC6pSkfF9eT/DeB0s6ArVs6azjWVdh9xRB0f5oTOMwGUi6CBZNKM
1wDWXTeWXzLY+deftQsuHT7aSxlG15MicigKKEMqxTmolG7K+zroOIz0oyu39bYe
gU1iiy/F9HEVrYeEUrh/eN77D32XwxECbyhAHC7olMdI4m/8IRgp5ONZAoGBAIzY
wfZKet5kQXfQSLHZzJw/0HKbAMdPBf6jmJiCDzNGCQ6goGMK994ArstxJD1MuTFH
nlrbFyzZgBJErl24RWsyF0z6gx6JdEEIdanD1WeEoN01JhX134lmfi5K5dxyJpb8
g3t1w6YL55932ch1IO3tBCNAWmYF25L2/lw9lZ8xAoGBAN/iuNv6GgOImgAZLA7a
qZ5srrPwC7IrP69Tkisdxi/QmMMeLRvEMsBo+LCNHVSPMn0C5it8fWB2vEGTgfj9
qgw66KtjKG2iIIfYpYA1aNbzDCElez231zbBWTxqlp2SJvG994bAu5dSxg8GBf43
8b4/NGaLXbIl56MED8+MxxPK
-----END PRIVATE KEY-----`

func TestLoadPrivateKey(t *testing.T) {
	// Inline raw PEM
	pem, err := loadPrivateKey(global.GitHubAppConfig{PrivateKey: testKeyPEM})
	if err != nil {
		t.Fatalf("raw PEM: %v", err)
	}
	if string(pem) != testKeyPEM {
		t.Fatal("raw PEM roundtrip mismatch")
	}

	// Base64-encoded PEM
	encoded := base64.StdEncoding.EncodeToString([]byte(testKeyPEM))
	pem, err = loadPrivateKey(global.GitHubAppConfig{PrivateKey: encoded})
	if err != nil {
		t.Fatalf("base64 PEM: %v", err)
	}
	if string(pem) != testKeyPEM {
		t.Fatal("base64 PEM roundtrip mismatch")
	}

	// From file path
	dir := t.TempDir()
	path := filepath.Join(dir, "app.pem")
	if err := os.WriteFile(path, []byte(testKeyPEM), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadPrivateKey(global.GitHubAppConfig{PrivateKeyPath: path}); err != nil {
		t.Fatalf("file PEM: %v", err)
	}

	// Missing config
	if _, err = loadPrivateKey(global.GitHubAppConfig{}); err == nil {
		t.Fatal("expected error for missing key config")
	}
}

func TestNewAppTokenSourceParsesKey(t *testing.T) {
	if _, err := newAppTokenSource(global.GitHubAppConfig{
		AppID:          123,
		InstallationID: 456,
		PrivateKey:     testKeyPEM,
	}, "https://api.github.com"); err != nil {
		t.Fatalf("valid key should parse: %v", err)
	}

	if _, err := newAppTokenSource(global.GitHubAppConfig{
		AppID:          123,
		InstallationID: 456,
		PrivateKey:     "not a key",
	}, "https://api.github.com"); err == nil {
		t.Fatal("invalid key should fail")
	}
}

func TestAppJWTHasExpectedClaims(t *testing.T) {
	src, err := newAppTokenSource(global.GitHubAppConfig{
		AppID:          123,
		InstallationID: 456,
		PrivateKey:     testKeyPEM,
	}, "https://api.github.com")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := src.appJWT()
	if err != nil {
		t.Fatalf("appJWT: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty JWT")
	}
}
