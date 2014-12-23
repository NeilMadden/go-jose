/*-
 * Copyright 2014 Square Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package jose

import (
	"encoding/json"
	"fmt"
	"strings"
)

// rawJsonWebEncryption represents a raw JWE JSON object. Used for parsing/serializing.
type rawJsonWebEncryption struct {
	Protected    *encodedBuffer     `json:"protected,omitempty"`
	Unprotected  *Header            `json:"unprotected,omitempty"`
	Header       *Header            `json:"header,omitempty"`
	Recipients   []rawRecipientInfo `json:"recipients,omitempty"`
	Aad          *encodedBuffer     `json:"aad,omitempty"`
	EncryptedKey *encodedBuffer     `json:"encrypted_key,omitempty"`
	Iv           *encodedBuffer     `json:"iv,omitempty"`
	Ciphertext   *encodedBuffer     `json:"ciphertext,omitempty"`
	Tag          *encodedBuffer     `json:"tag,omitempty"`
}

// rawRecipientInfo represents a raw JWE Per-Recipient Header JSON object. Used for parsing/serializing.
type rawRecipientInfo struct {
	Header       *Header `json:"header,omitempty"`
	EncryptedKey string  `json:"encrypted_key,omitempty"`
}

// JsonWebEncryption represents an encrypted JWE object after parsing.
type JsonWebEncryption struct {
	protected, unprotected   *Header
	recipients               []recipientInfo
	aad, iv, ciphertext, tag []byte
	original                 *rawJsonWebEncryption
}

// recipientInfo represents a raw JWE Per-Recipient Header JSON object after parsing.
type recipientInfo struct {
	header       *Header
	encryptedKey []byte
}

// GetAuthData retrieves the (optional) authenticated data attached to the object.
func (obj JsonWebEncryption) GetAuthData() []byte {
	if obj.aad != nil {
		out := make([]byte, len(obj.aad))
		copy(out, obj.aad)
		return out
	}

	return nil
}

// Get the merged header values
func (obj JsonWebEncryption) mergedHeaders(recipient *recipientInfo) Header {
	out := Header{}
	out.merge(obj.protected)
	out.merge(obj.unprotected)

	if recipient != nil {
		out.merge(recipient.header)
	}

	return out
}

// Get the additional authenticated data from a JWE object.
func (obj JsonWebEncryption) computeAuthData() []byte {
	var protected string

	if obj.original != nil {
		protected = obj.original.Protected.base64()
	} else {
		protected = base64URLEncode(mustSerializeJSON((obj.protected)))
	}

	output := []byte(protected)
	if obj.aad != nil {
		output = append(output, '.')
		output = append(output, []byte(base64URLEncode(obj.aad))...)
	}

	return output
}

// ParseEncrypted parses an encrypted message in compact or full serialization format.
func ParseEncrypted(input string) (*JsonWebEncryption, error) {
	input = stripWhitespace(input)
	if strings.HasPrefix(input, "{") {
		return parseEncryptedFull(input)
	}

	return parseEncryptedCompact(input)
}

// parseEncryptedFull parses a message in compact format.
func parseEncryptedFull(input string) (*JsonWebEncryption, error) {
	var parsed rawJsonWebEncryption
	err := json.Unmarshal([]byte(input), &parsed)
	if err != nil {
		return nil, err
	}

	obj := &JsonWebEncryption{}
	obj.original = &parsed
	obj.unprotected = parsed.Unprotected

	if parsed.Protected != nil && len(parsed.Protected.bytes()) > 0 {
		err = json.Unmarshal(parsed.Protected.bytes(), &obj.protected)
		if err != nil {
			return nil, fmt.Errorf("square/go-jose: invalid protected header: %s, %s", err, parsed.Protected.base64())
		}
	}

	if len(parsed.Recipients) == 0 {
		obj.recipients = []recipientInfo{
			recipientInfo{
				header:       parsed.Header,
				encryptedKey: parsed.EncryptedKey.bytes(),
			},
		}
	} else {
		obj.recipients = make([]recipientInfo, len(parsed.Recipients))
		for r := range parsed.Recipients {
			encryptedKey, err := base64URLDecode(parsed.Recipients[r].EncryptedKey)
			if err != nil {
				return nil, err
			}

			obj.recipients[r].header = parsed.Recipients[r].Header
			obj.recipients[r].encryptedKey = encryptedKey
		}
	}

	for _, recipient := range obj.recipients {
		headers := obj.mergedHeaders(&recipient)
		if headers.Alg == "" || headers.Enc == "" {
			return nil, fmt.Errorf("square/go-jose: message is missing alg/enc headers")
		}
	}

	obj.iv = parsed.Iv.bytes()
	obj.ciphertext = parsed.Ciphertext.bytes()
	obj.tag = parsed.Tag.bytes()
	obj.aad = parsed.Aad.bytes()

	return obj, nil
}

// parseEncryptedCompact parses a message in compact format.
func parseEncryptedCompact(input string) (*JsonWebEncryption, error) {
	parts := strings.Split(input, ".")
	if len(parts) != 5 {
		return nil, fmt.Errorf("square/go-jose: compact JWE format must have five parts")
	}

	rawProtected, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, err
	}

	var protected Header
	err = json.Unmarshal(rawProtected, &protected)
	if err != nil {
		return nil, err
	}

	if protected.Alg == "" || protected.Enc == "" {
		return nil, fmt.Errorf("square/go-jose: message is missing alg/enc headers")
	}

	encryptedKey, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, err
	}

	iv, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, err
	}

	ciphertext, err := base64URLDecode(parts[3])
	if err != nil {
		return nil, err
	}

	tag, err := base64URLDecode(parts[4])
	if err != nil {
		return nil, err
	}

	return &JsonWebEncryption{
		protected: &protected,
		recipients: []recipientInfo{
			recipientInfo{
				encryptedKey: encryptedKey,
			},
		},
		iv:         iv,
		ciphertext: ciphertext,
		tag:        tag,
		original: &rawJsonWebEncryption{
			Protected:    newBuffer(rawProtected),
			EncryptedKey: newBuffer(encryptedKey),
			Iv:           newBuffer(iv),
			Ciphertext:   newBuffer(ciphertext),
			Tag:          newBuffer(tag),
		},
	}, nil
}

// CompactSerialize serializes an object using the compact serialization format.
func (obj JsonWebEncryption) CompactSerialize() (string, error) {
	if len(obj.recipients) > 1 || obj.unprotected != nil || obj.recipients[0].header != nil {
		return "", ErrNotSupported
	}

	serializedProtected := mustSerializeJSON(obj.protected)

	return fmt.Sprintf(
		"%s.%s.%s.%s.%s",
		base64URLEncode(serializedProtected),
		base64URLEncode(obj.recipients[0].encryptedKey),
		base64URLEncode(obj.iv),
		base64URLEncode(obj.ciphertext),
		base64URLEncode(obj.tag)), nil
}

// FullSerialize serializes an object using the full JSON serialization format.
func (obj JsonWebEncryption) FullSerialize() string {
	raw := rawJsonWebEncryption{
		Unprotected:  obj.unprotected,
		Iv:           newBuffer(obj.iv),
		Ciphertext:   newBuffer(obj.ciphertext),
		EncryptedKey: newBuffer(obj.recipients[0].encryptedKey),
		Tag:          newBuffer(obj.tag),
		Aad:          newBuffer(obj.aad),
		Recipients:   []rawRecipientInfo{},
	}

	if len(obj.recipients) > 1 {
		for _, recipient := range obj.recipients {
			info := rawRecipientInfo{
				Header:       recipient.header,
				EncryptedKey: base64URLEncode(recipient.encryptedKey),
			}
			raw.Recipients = append(raw.Recipients, info)
		}
	} else {
		// Use flattened serialization
		raw.Header = obj.recipients[0].header
		raw.EncryptedKey = newBuffer(obj.recipients[0].encryptedKey)
	}

	raw.Protected = newBuffer(mustSerializeJSON(obj.protected))

	return string(mustSerializeJSON(raw))
}
