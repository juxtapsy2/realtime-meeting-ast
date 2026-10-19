package intelligence

import (
	"encoding/json"
	"fmt"
	"strings"
)

// finalizationPrompt is the shared finalization prompt for every provider.
// Its output contract is deliberately narrow: the application renders the MOM
// header line itself and reads only title/summary/mom_entries/key_points, so
// the prompt forbids prose, fences, and any other keys. This keeps completion
// tokens bounded and makes parsing deterministic.
func finalizationPrompt(glossary Glossary, input FinalizationInput) string {
	stateJSON := "none"
	if input.FinalState != nil {
		if state, err := json.MarshalIndent(input.FinalState, "", "  "); err == nil {
			stateJSON = string(state)
		}
	}

	documents := sanitizePromptText(input.DocumentText)
	if documents == "" {
		documents = "none"
	}
	transcript := sanitizePromptText(input.FullTranscript)
	if transcript == "" {
		transcript = "none"
	}

	return fmt.Sprintf(`You are an AI meeting assistant. Produce the Minutes of Meeting (MOM) for one meeting from the evidence below.

Meeting title: %s

%s
Final Meeting State:
%s

Full Transcript:
%s

Meeting Documents:
%s

Write the MOM entries in this exact template, one entry per discussed item:

28/09/2026 <meeting title> MOM:
[ITEM_ID] Item title: current status of the item.

  Actions: next action needed, ETA: expected completion period

Template-to-JSON mapping - each template entry becomes one "mom_entries" element:
- [ITEM_ID] -> "id": the identifier exactly as written in the evidence (e.g. [CR_002], [PADM2-160443], [INC-1489047], [HLP-2026-169915]). Never invent, change, or replace identifiers with sequence numbers; "" when none exists; the application adds the brackets.
- "Item title" -> "title": the short item title without the identifier.
- "current status of the item" -> "status": state the CURRENT STATUS clearly (what has been confirmed/done/decided so far).
- "next action needed, ETA: expected completion period" -> "actions": the NEXT ACTIONS as a single line; "" when no action is needed. Put who is responsible in "assignee" and the ETA in "eta" when mentioned.

Rules:
- Cover EVERY item in the evidence: one mom_entries element per CR, issue, request, or topic mentioned. Never drop items to keep the output short, and never cut an entry off mid-sentence - the MoM must account for the whole document.
- The application renders the opening line "DD/MM/YYYY <meeting title> MOM:" and the bullet markers from the meeting end time; never output them, dates, or speaker labels.
- Output one JSON object and nothing else: no markdown fences, no text before or after it.
- Exactly these four keys: "title", "summary", "mom_entries", "key_points". Do not include decisions, action_items, issues, questions, participants, or duration arrays.
- "title": the meeting title above; when empty, a short factual title from the evidence.
- "summary": one or two sentences on the meeting outcome.
- "key_points": at most 5 bullets, at most 12 words each.
- The transcript may be empty (the meeting was never recorded). In that case derive the MOM entirely from the meeting documents; when both are present, use the documents to complete what the transcript missed.
- Use the regulated business glossary terms EXACTLY as written. Never guess or substitute similar-sounding words for glossary terms.
- Preserve the speakers' original language. If the meeting mixed Vietnamese and English, keep that mix in each entry.
- Ignore filler words and hesitations ("um", "uh", "ừ", "ờ", repetitions); they carry no meaning for the MOM.
- Keep entries terse; no commentary outside the MOM format.

Example:
{"title":"Weekly sync","summary":"CR_002 budget approved; test plan still pending.","mom_entries":[{"id":"CR_002","title":"budget","status":"approved by Linh","actions":"publish the decision log, ETA: Friday","assignee":"Linh","eta":"Friday"}],"key_points":["CR_002 budget approved"]}`,
		input.Title, glossary.PromptSection(), stateJSON, transcript, documents)
}

// momSummaryFormat constrains Groq/OpenAI finalization output to exactly the
// fields the MoM template consumes. Strict mode rejects unknown keys and prose,
// so the provider enforces what the prompt asks for.
func momSummaryFormat() map[string]interface{} {
	str := map[string]interface{}{"type": "string"}
	return map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "mom_summary",
			"strict": true,
			"schema": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"title":   str,
					"summary": str,
					"mom_entries": map[string]interface{}{
						"type":  "array",
						"items": momEntrySchema(),
					},
					"key_points": map[string]interface{}{
						"type":  "array",
						"items": str,
					},
				},
				"required": []string{"title", "summary", "mom_entries", "key_points"},
			},
		},
	}
}

// momEntrySchema is the single MoM entry shape, shared by the full summary
// schema and the chunk-extraction schema.
func momEntrySchema() map[string]interface{} {
	str := map[string]interface{}{"type": "string"}
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"id":       str,
			"title":    str,
			"status":   str,
			"actions":  str,
			"assignee": str,
			"eta":      str,
		},
		"required": []string{"id", "title", "status", "actions", "assignee", "eta"},
	}
}

// chunkEntriesFormat is the map-step schema: only the fields that later get
// merged into the final MoM, keeping each chunk's completion small.
func chunkEntriesFormat() map[string]interface{} {
	str := map[string]interface{}{"type": "string"}
	return map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "mom_chunk_entries",
			"strict": true,
			"schema": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"mom_entries": map[string]interface{}{
						"type":  "array",
						"items": momEntrySchema(),
					},
					"key_points": map[string]interface{}{
						"type":  "array",
						"items": str,
					},
				},
				"required": []string{"mom_entries", "key_points"},
			},
		},
	}
}

// chunkExtractionPrompt asks for the MoM entries of one evidence chunk,
// using the same entry contract as the full finalization prompt.
func chunkExtractionPrompt(title string, glossary Glossary, chunkIndex, chunkCount int, chunk string) string {
	return fmt.Sprintf(`You are an AI meeting assistant. Extract the Minutes of Meeting (MOM) entries from PART %d of %d of one meeting's evidence.

Meeting title: %s

%s
Evidence part %d of %d:
%s

Cover EVERY item mentioned in this evidence part: one entry per CR, issue, request, or topic. Do not summarize away items.

Entry rules (same as the full MOM template):
- "id": the identifier exactly as written in the evidence (e.g. [CR_002], [PADM2-160443], [INC-1489047], [HLP-2026-169915]). Never invent, change, or replace identifiers with sequence numbers; "" when none exists.
- "title": the short item title without the identifier.
- "status": the CURRENT STATUS (what has been confirmed/done/decided so far).
- "actions": the NEXT ACTIONS as a single line; "" when none. Put who is responsible in "assignee" and the ETA in "eta" when mentioned.
- "key_points": at most 3 short bullets for this part.
- Use regulated business glossary terms EXACTLY as written.
- Preserve the speakers' original language (Vietnamese/English mix stays as spoken).
- Ignore filler words and hesitations ("um", "uh", "ừ", "ờ", repetitions).
- Output one JSON object with "mom_entries" and "key_points" and nothing else. No markdown fences.`,
		chunkIndex, chunkCount, title, glossary.PromptSection(), chunkIndex, chunkCount, chunk)
}

// parseChunkEntries parses one map-step response.
func parseChunkEntries(response string) (entries []MOMEntry, keyPoints []string, err error) {
	var chunk struct {
		MOMEntries []MOMEntry `json:"mom_entries"`
		KeyPoints  []string   `json:"key_points"`
	}
	jsonStr := extractJSON(response)
	if strings.TrimSpace(jsonStr) == "" {
		return nil, nil, fmt.Errorf("LLM returned an empty JSON response")
	}
	if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
		return nil, nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return chunk.MOMEntries, chunk.KeyPoints, nil
}

// mergePrompt assembles the final single-call MoM request from the entries
// extracted per chunk; the merged payload is far smaller than the raw
// evidence, so it fits the provider's per-request cap.
func mergePrompt(title string, glossary Glossary, entries []MOMEntry, keyPoints []string) string {
	entriesJSON, _ := json.Marshal(entries)
	keyJSON, _ := json.Marshal(keyPoints)
	if title == "" {
		title = "(unknown)"
	}
	return fmt.Sprintf(`You are an AI meeting assistant. Produce the final Minutes of Meeting (MOM) JSON for one meeting by merging the pre-extracted entries below. All evidence has already been reviewed; do not drop entries.

Meeting title: %s

%s
Pre-extracted MOM entries (from all evidence parts):
%s

Pre-extracted key points:
%s

Write the MOM entries in this exact template, one entry per item:

28/09/2026 <meeting title> MOM:
[ITEM_ID] Item title: current status of the item.

  Actions: next action needed, ETA: expected completion period

Rules:
- Keep EVERY provided entry; merge only entries that clearly describe the same item (same identifier or identical title+status).
- The application renders the opening line "DD/MM/YYYY <meeting title> MOM:" and the bullet markers; never output them, dates, or speaker labels.
- Output one JSON object and nothing else: no markdown fences. Exactly these four keys: "title", "summary", "mom_entries", "key_points".
- "title": the meeting title above; when empty, a short factual title from the entries.
- "summary": one or two sentences on the meeting outcome.
- "key_points": at most 5 bullets, at most 12 words each.
- Keep entries terse; no commentary outside the MOM format.

Example:
{"title":"Weekly sync","summary":"CR_002 budget approved; test plan still pending.","mom_entries":[{"id":"CR_002","title":"budget","status":"approved by Linh","actions":"publish the decision log, ETA: Friday","assignee":"Linh","eta":"Friday"}],"key_points":["CR_002 budget approved"]}`,
		title, glossary.PromptSection(), string(entriesJSON), string(keyJSON))
}

// parseFinalizationJSON converts a provider response into a MeetingSummary.
// An empty JSON document (e.g. a model that emitted only an empty code fence)
// gets its own error instead of the opaque "unexpected end of JSON input".
func parseFinalizationJSON(response string) (MeetingSummary, error) {
	var summary MeetingSummary

	jsonStr := extractJSON(response)
	if strings.TrimSpace(jsonStr) == "" {
		return MeetingSummary{}, fmt.Errorf("LLM returned an empty JSON response")
	}
	if err := json.Unmarshal([]byte(jsonStr), &summary); err != nil {
		return MeetingSummary{}, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return summary, nil
}
