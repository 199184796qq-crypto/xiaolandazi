package deviceops

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const PurposeDiagnostics = "diagnostics-upload"
const PurposeFirmware = "firmware-download"

type Grant struct {
	Version        int      `json:"version"`
	Purpose        string   `json:"purpose"`
	TenantID       int64    `json:"tenant_id"`
	DeviceID       int64    `json:"device_id"`
	OwnerEpoch     string   `json:"owner_epoch"`
	IssuedAt       int64    `json:"issued_at"`
	ExpiresAt      int64    `json:"expires_at"`
	ArtifactSHA256 string   `json:"artifact_sha256,omitempty"`
	Profile        *Profile `json:"profile,omitempty"`
}
type Signer struct{ secret []byte }

func NewSigner(secret string) (*Signer, error) {
	if len(strings.TrimSpace(secret)) < 16 {
		return nil, errors.New("设备维护授权密钥未正确配置")
	}
	return &Signer{[]byte(secret)}, nil
}
func (s *Signer) signature(payload string) []byte {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte("device-maintenance-v1\x00" + payload))
	return h.Sum(nil)
}
func (s *Signer) Issue(g Grant, now time.Time) (string, error) {
	g.Version = 1
	g.IssuedAt = now.Unix()
	g.ExpiresAt = now.Add(GrantTTL).Unix()
	if err := g.validate(now, g.Purpose); err != nil {
		return "", err
	}
	raw, err := json.Marshal(g)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + base64.RawURLEncoding.EncodeToString(s.signature(payload)), nil
}
func (s *Signer) Verify(token, purpose string, now time.Time) (Grant, error) {
	var g Grant
	if len(token) > 4096 {
		return g, errors.New("设备维护授权无效或已过期")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return g, errors.New("设备维护授权无效或已过期")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, s.signature(parts[0])) {
		return g, errors.New("设备维护授权无效或已过期")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || StrictJSON(raw, &g, 3072) != nil {
		return g, errors.New("设备维护授权无效或已过期")
	}
	return g, g.validate(now, purpose)
}
func (g Grant) validate(now time.Time, purpose string) error {
	if g.Version != 1 || g.Purpose != purpose || (purpose != PurposeDiagnostics && purpose != PurposeFirmware) || g.TenantID <= 0 || g.DeviceID <= 0 || !HexID(g.OwnerEpoch, 64, 64) || g.IssuedAt > now.Unix()+1 || g.ExpiresAt <= now.Unix() || g.ExpiresAt-g.IssuedAt > int64(GrantTTL/time.Second) || g.ExpiresAt <= g.IssuedAt {
		return errors.New("设备维护授权无效或已过期")
	}
	if purpose == PurposeFirmware {
		if !HexID(g.ArtifactSHA256, 64, 64) || g.Profile == nil || g.Profile.Validate() != nil {
			return errors.New("固件下载授权无效")
		}
	} else if g.ArtifactSHA256 != "" || g.Profile != nil {
		return errors.New("日志上传授权用途不匹配")
	}
	return nil
}
func (g Grant) Identity() Identity {
	return Identity{TenantID: g.TenantID, DeviceID: g.DeviceID, OwnerEpoch: g.OwnerEpoch}
}
