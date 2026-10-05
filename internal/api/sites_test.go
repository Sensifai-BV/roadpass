package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sensifai/roadpass/internal/domain"
)

func TestSiteIn(t *testing.T) {
	ok := map[string]string{
		"own site":     `{"name":"North yard","kind":"recycler","lat":52.37,"lon":4.9}`,
		"partner site": `{"name":"Main plant","kind":"asphalt_plant","partner":" Asfaltos SL ","country":"es"}`,
		"other type":   `{"name":"Transfer","kind":"other","kindOther":"Transfer station","partner":"Acme"}`,
	}
	w := httptest.NewRecorder()
	if x, good := siteIn(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Yard","lat":1,"lon":1}`)), domain.KindRecycler, ""); !good || x.Kind != "recycler" {
		t.Errorf("untyped site does not take the organisation's kind: %+v %s", x, w.Body)
	}
	for name, body := range ok {
		w := httptest.NewRecorder()
		x, good := siteIn(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "", "")
		if !good {
			t.Errorf("%s rejected: %s", name, w.Body)
		}
		if name == "partner site" && (x.Partner != "Asfaltos SL" || x.Country != "ES" || x.Own()) {
			t.Errorf("partner site not normalised: %+v", x)
		}
	}
	bad := map[string]string{
		"no name":              `{"kind":"recycler","lat":1,"lon":1}`,
		"no type":              `{"name":"x","lat":1,"lon":1}`,
		"unknown type":         `{"name":"x","kind":"auditor","lat":1,"lon":1}`,
		"other without text":   `{"name":"x","kind":"other","partner":"Acme"}`,
		"own without location": `{"name":"x","kind":"recycler"}`,
		"bad country":          `{"name":"x","kind":"lab","partner":"Acme","country":"ESP"}`,
	}
	for name, body := range bad {
		w := httptest.NewRecorder()
		if _, good := siteIn(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), domain.KindAuditor, ""); good || w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s accepted (status %d)", name, w.Code)
		}
	}
	legacy := `{"name":"Office","kind":"auditor","lat":1,"lon":1}`
	if _, good := siteIn(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/", strings.NewReader(legacy)), domain.KindAuditor, "auditor"); !good {
		t.Error("an older site cannot keep its organisation-kind type")
	}
	if _, good := siteIn(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/", strings.NewReader(legacy)), domain.KindAuditor, "lab"); good {
		t.Error("a site can be changed to a type outside the site types")
	}
}
