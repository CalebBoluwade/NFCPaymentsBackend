# HSM Security Configuration Guide

## Key Rotation (KeyRotationDays)

### How It Works
The `KeyRotationDays` parameter controls automatic key lifecycle management:

1. **Key Expiration**: Each key pair has an `ExpiresAt` timestamp set to 1 year from creation
2. **Rotation Check**: The `RotateKeys()` method checks if keys are expired or inactive
3. **Automatic Rotation**: When a key expires, a new key is generated with timestamp suffix
4. **Graceful Transition**: Old keys are marked inactive but not immediately deleted for verification of existing signatures

### Current Implementation
- Keys expire after **365 days** (hardcoded in `GenerateKeyPair`)
- `KeyRotationDays` parameter is defined but not actively used in rotation logic
- Rotation is triggered manually via `RotateKeys()` method

### Security Benefits
- **Forward Secrecy**: Compromised old keys can't decrypt new data
- **Compliance**: Meets regulatory requirements for key lifecycle
- **Risk Mitigation**: Limits exposure window if keys are compromised

## HSM_MASTER_KEY Secure Generation

### Critical Security Requirements
The master key is the root of all cryptographic security. It must be:
- **256-bit minimum** (32 bytes)
- **Cryptographically random**
- **Unique per environment**
- **Never stored in plaintext**

### Secure Generation Methods

#### Method 1: OpenSSL (Recommended)
```bash
# Generate 256-bit (32 bytes) random key
openssl rand -hex 32

# Example output:
# a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456
```

#### Method 2: Python
```python
import secrets
import base64

# Generate 32 random bytes and encode as hex
master_key = secrets.token_hex(32)
print(master_key)

# Or as base64
master_key_b64 = base64.b64encode(secrets.token_bytes(32)).decode()
print(master_key_b64)
```

#### Method 3: Go
```go
package main

import (
    "crypto/rand"
    "encoding/hex"
    "fmt"
)

func main() {
    key := make([]byte, 32)
    rand.Read(key)
    fmt.Printf("%x\n", key)
}
```

#### Method 4: Hardware Security Module
```bash
# If you have access to HSM
pkcs11-tool --generate-random 32 | xxd -p -c 32
```

### Key Storage Best Practices

#### Production Environment
```bash
# Store in secure key management service
export HSM_MASTER_KEY=$(aws secretsmanager get-secret-value \
  --secret-id prod/hsm/master-key \
  --query SecretString --output text)

# Or use HashiCorp Vault
export HSM_MASTER_KEY=$(vault kv get -field=master_key secret/hsm)
```

#### Development Environment
```bash
# Generate and store in .env (never commit)
echo "HSM_MASTER_KEY=$(openssl rand -hex 32)" >> .env.local
```

### Security Warnings

❌ **NEVER DO THIS:**
```bash
# Predictable patterns
HSM_MASTER_KEY="12345678901234567890123456789012"

# Dictionary words
HSM_MASTER_KEY="password123supersecret456789012345"

# Timestamps or sequential
HSM_MASTER_KEY="20240101000000000000000000000000"

# Short keys
HSM_MASTER_KEY="shortkey"
```

✅ **ALWAYS DO THIS:**
```bash
# Cryptographically random
HSM_MASTER_KEY="a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456"

# Environment-specific
HSM_MASTER_KEY_PROD="different-from-dev-and-staging"
HSM_MASTER_KEY_DEV="different-from-prod-and-staging"
```

### Key Derivation Security
The HSM uses Argon2ID for key derivation:
```go
// Master key is derived using Argon2ID with salt
masterKey := deriveKey(config.MasterKey, "master-salt", 32)
```

This provides:
- **Memory-hard function**: Resistant to GPU attacks
- **Salt protection**: Prevents rainbow table attacks
- **Configurable cost**: Adjustable security vs performance

### Compliance Notes
- **PCI DSS**: Requires key rotation and secure generation
- **FIPS 140-2**: Use certified random number generators
- **Common Criteria**: Document key lifecycle procedures
- **SOX**: Maintain audit trail of key operations