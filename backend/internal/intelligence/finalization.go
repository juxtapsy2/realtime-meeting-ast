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

	documents := input.DocumentText
	if documents == "" {
		documents = "none"
	}
	transcript := input.FullTranscript
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
						"type": "array",
						"items": map[string]interface{}{
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
						},
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
