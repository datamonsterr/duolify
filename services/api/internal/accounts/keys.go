package accounts

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

func authKey(secret, purpose string) []byte {
	// Fixed length is within HKDF's bounds for SHA-256.
	key, err := hkdf.Key(sha256.New, []byte(secret), nil, "duolify/auth/v1/"+purpose, 32)
	if err != nil {
		panic(err)
	}
	return key
}

func (s *Service) credentialCipher() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.sealKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *Service) seal(raw []byte, context string) ([]byte, error) {
	aead, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, raw, []byte(context)), nil
}

func (s *Service) unseal(sealed []byte, context string) ([]byte, error) {
	aead, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, errors.New("invalid credential ciphertext")
	}
	return aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(context))
}
