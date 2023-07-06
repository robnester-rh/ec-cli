// Copyright 2023 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package attestation

import (
	"encoding/base64"
	"encoding/json"
	"io"

	"github.com/in-toto/in-toto-golang/in_toto"
	v02 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	"github.com/sigstore/cosign/v2/pkg/oci"
	"github.com/sigstore/cosign/v2/pkg/types"

	"github.com/enterprise-contract/ec-cli/internal/signature"
)

// ProvenanceStatementSLSA02 extends in_toto.ProvenanceStatementSLSA02 to provide the
// Extra attribute.
type ProvenanceStatementSLSA02 struct {
	in_toto.ProvenanceStatementSLSA02
	Extra extra `json:"extra"`
}

// SLSAProvenanceFromSignature parses the SLSA Provenance v0.2 from the provided OCI
// layer. Expects that the layer contains DSSE JSON with the embeded SLSA
// Provenance v0.2 payload.
func SLSAProvenanceFromSignature(sig oci.Signature) (Attestation, error) {
	if sig == nil {
		return nil, AT001
	}
	typ, err := sig.MediaType()
	if err != nil {
		return nil, AT002.CausedBy(err)
	}

	if typ != types.DssePayloadType {
		return nil, AT002.CausedByF("Expecting media type of `%s`, received: `%s`", types.DssePayloadType, typ)
	}

	reader, err := sig.Uncompressed()
	if err != nil {
		return nil, AT002.CausedBy(err)
	}
	defer reader.Close()

	payloadBytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, AT002.CausedBy(err)
	}

	var payload cosign.AttestationPayload
	err = json.Unmarshal(payloadBytes, &payload)
	if err != nil {
		return nil, AT002.CausedBy(err)
	}

	if payload.PayLoad == "" {
		return nil, AT002.CausedByF("No `payload` data found")
	}

	embeded, err := base64.StdEncoding.DecodeString(payload.PayLoad)
	if err != nil {
		return nil, AT002.CausedBy(err)
	}

	var statement ProvenanceStatementSLSA02
	if err := json.Unmarshal(embeded, &statement); err != nil {
		return nil, AT002.CausedBy(err)
	}

	if statement.Type != in_toto.StatementInTotoV01 {
		return nil, AT003.CausedByF(statement.Type)
	}

	if statement.PredicateType != v02.PredicateSLSAProvenance {
		return nil, AT004.CausedByF(statement.PredicateType)
	}

	statement.Extra.Signatures, err = createEntitySignatures(sig, statement, payload)
	if err != nil {
		return nil, AT005.CausedBy(err)
	}

	return slsaProvenance{statement: statement, payload: payload, bytes: embeded}, nil
}

func createEntitySignatures(sig oci.Signature, statement ProvenanceStatementSLSA02, payload cosign.AttestationPayload) ([]signature.EntitySignature, error) {
	es, err := signature.NewEntitySignature(sig)
	if err != nil {
		return nil, err
	}

	es.Metadata["predicateType"] = statement.PredicateType
	es.Metadata["type"] = statement.Type
	if statement.Predicate.BuildType != "" {
		es.Metadata["predicateBuildType"] = statement.Predicate.BuildType
	}

	var out []signature.EntitySignature
	for _, s := range payload.Signatures {
		esNew := es
		// The Signature and KeyID can come from two locations, the oci.Signature or
		// the cosign.Signature. In some cases, both are filled in, while in others
		// only one location contains the value. The discrepancy can be seen when
		// comparing signatures created via keyless vs long-lived key workflows. Here
		// we prioritize the information from oci.Signature, but fallback when needed
		// to cosign.Signature. (This inconsistency is likely a bug in cosign)
		if esNew.Signature == "" {
			esNew.Signature = s.Sig
		}
		if esNew.KeyID == "" {
			esNew.KeyID = s.KeyID
		}
		out = append(out, esNew)
	}
	return out, nil
}

type slsaProvenance struct {
	statement ProvenanceStatementSLSA02
	payload   cosign.AttestationPayload
	bytes     []byte
}

func (a slsaProvenance) Data() []byte {
	return a.bytes
}

func (a slsaProvenance) Statement() any {
	return a.statement
}

func (a slsaProvenance) Signatures() []signature.EntitySignature {
	return a.statement.Extra.Signatures
}
