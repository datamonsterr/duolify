package characters

import "testing"

func TestPromptDraftUsesOnlyValidCatalogChoices(t *testing.T) {
	for _, kind := range []string{"human", "animal", "robot", "fantasy"} {
		doc, err := promptDraft(`{"attributes":[{"field":"body.kind","value":"` + kind + `"}]}`)
		if err != nil || doc.Body.Kind != kind || doc.Identity != (Identity{}) {
			t.Fatalf("%s draft: %+v, %v", kind, doc, err)
		}
	}
	doc, err := promptDraft(`{"attributes":[{"field":"body.kind","value":"robot"},{"field":"body.height","value":"tall"},{"field":"clothes.accessories.scarf","value":"#FF0000"}]}`)
	if err != nil || doc.Body.Height != "tall" || len(doc.Clothes.Accessories) != 1 ||
		doc.Clothes.Accessories[0] != (Accessory{Kind: "scarf", Color: "#FF0000"}) {
		t.Fatalf("robot draft: %+v, %v", doc, err)
	}
	for _, raw := range []string{
		`{"attributes":[{"field":"body.race","value":"human"}]}`,
		`{"attributes":[{"field":"info.name","value":"Sam"}]}`,
		`{"attributes":[{"field":"body.kind","value":"robot"},{"field":"body.hair.style","value":"braids"}]}`,
		`{"attributes":[{"field":"body.eyes.color","value":"https://example.org"}]}`,
		`{"attributes":[{"field":"body.age_band","value":"child"},{"field":"clothes.top.style","value":"none"}]}`,
	} {
		if _, err := promptDraft(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	partial := partialPromptDraft(`{"attributes":[{"field":"body.height","value":"tall"},{"field":"body.eyes.color","value":"#000000"},{"field":"body.kind","value":"robot"},{"field":"info.name","value":"Sam"}]}`)
	if partial == nil || partial.Body.Kind != "robot" || partial.Body.Height != "tall" ||
		partial.Body.Eyes.Color == "#000000" || partial.Info.Name == "Sam" {
		t.Fatalf("partial recovery kept an incompatible or free-text value: %+v", partial)
	}
}
