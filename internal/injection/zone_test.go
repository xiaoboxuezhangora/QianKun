package injection

import (
	"strings"
	"testing"
)

func TestExtractZonesReturnsNoneWhenMarkersAbsent(t *testing.T) {
	zones, err := ExtractZones("plain user instructions")
	if err != nil {
		t.Fatalf("extract zones: %v", err)
	}
	if len(zones) != 0 {
		t.Fatalf("expected no zones, got %+v", zones)
	}
}

func TestExtractZonesSingleZoneOnlyIncludesMarkedContent(t *testing.T) {
	input := "before\n" + StartMarker + "\ninjected\n" + EndMarker + "\nafter"

	zones, err := ExtractZones(input)
	if err != nil {
		t.Fatalf("extract zones: %v", err)
	}
	if len(zones) != 1 {
		t.Fatalf("expected one zone, got %+v", zones)
	}
	if zones[0].Content != "\ninjected\n" {
		t.Fatalf("unexpected content %q", zones[0].Content)
	}
	if strings.Contains(zones[0].Content, "before") || strings.Contains(zones[0].Content, "after") {
		t.Fatalf("zone content included text outside markers: %q", zones[0].Content)
	}
}

func TestExtractZonesMultipleZones(t *testing.T) {
	input := StartMarker + "one" + EndMarker + "\nuser\n" + StartMarker + "two" + EndMarker

	zones, err := ExtractZones(input)
	if err != nil {
		t.Fatalf("extract zones: %v", err)
	}
	if len(zones) != 2 {
		t.Fatalf("expected two zones, got %+v", zones)
	}
	if zones[0].Content != "one" || zones[1].Content != "two" {
		t.Fatalf("unexpected zone contents: %+v", zones)
	}
}

func TestExtractZonesMissingEndReturnsClearError(t *testing.T) {
	_, err := ExtractZones("before\n" + StartMarker + "\ninjected")
	if err == nil {
		t.Fatal("expected missing end marker error")
	}
	if !strings.Contains(err.Error(), "missing end marker") || !strings.Contains(err.Error(), EndMarker) {
		t.Fatalf("expected clear missing end marker error, got %v", err)
	}
}

func TestExtractZonesEndBeforeStartReturnsError(t *testing.T) {
	_, err := ExtractZones("before\n" + EndMarker + "\n" + StartMarker + "content" + EndMarker)
	if err == nil {
		t.Fatal("expected unmatched end marker error")
	}
	if !strings.Contains(err.Error(), "no matching start marker") && !strings.Contains(err.Error(), "appears before") {
		t.Fatalf("expected clear unmatched end marker error, got %v", err)
	}
}
