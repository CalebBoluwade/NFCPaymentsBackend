-- Function to insert or update HSM keys from the HSM service
CREATE OR REPLACE FUNCTION upsert_hsm_key(
    p_key_id VARCHAR(255),
    p_key_type VARCHAR(50),
    p_key_usage VARCHAR(50),
    p_key_size INTEGER,
    p_public_key TEXT,
    p_encrypted_private_key TEXT,
    p_expires_at TIMESTAMP,
    p_metadata JSONB DEFAULT NULL
) RETURNS VOID AS $$
BEGIN
    INSERT INTO hsm_keys (
        key_id, key_type, key_usage, key_size, 
        public_key, encrypted_private_key, 
        is_active, expires_at, metadata
    ) VALUES (
        p_key_id, p_key_type, p_key_usage, p_key_size,
        p_public_key, p_encrypted_private_key,
        true, p_expires_at, p_metadata
    )
    ON CONFLICT (key_id) DO UPDATE SET
        public_key = EXCLUDED.public_key,
        encrypted_private_key = EXCLUDED.encrypted_private_key,
        is_active = EXCLUDED.is_active,
        expires_at = EXCLUDED.expires_at,
        metadata = EXCLUDED.metadata;
END;
$$ LANGUAGE plpgsql;