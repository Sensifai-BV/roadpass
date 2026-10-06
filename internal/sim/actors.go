package sim

import "github.com/sensifai/roadpass/internal/domain"

// Town is a real Spanish municipality with an industrial estate (research §9).
// Coordinates are approximate municipal centroids.
type Town struct {
	Name, Province, Region string
	Lat, Lon               float64
}

// Region names (autonomous communities).
const (
	rAND = "Andalucía"
	rARA = "Aragón"
	rAST = "Principado de Asturias"
	rCNB = "Cantabria"
	rCYL = "Castilla y León"
	rCLM = "Castilla-La Mancha"
	rCAT = "Cataluña"
	rVAL = "Comunitat Valenciana"
	rEXT = "Extremadura"
	rGAL = "Galicia"
	rMAD = "Comunidad de Madrid"
	rMUR = "Región de Murcia"
	rNAV = "Comunidad Foral de Navarra"
	rPV  = "País Vasco"
	rRIO = "La Rioja"
)

var towns = map[string]Town{
	"Alcalá de Guadaíra":   {"Alcalá de Guadaíra", "Sevilla", rAND, 37.34, -5.84},
	"Dos Hermanas":         {"Dos Hermanas", "Sevilla", rAND, 37.28, -5.92},
	"Antequera":            {"Antequera", "Málaga", rAND, 37.02, -4.56},
	"Málaga":               {"Málaga", "Málaga", rAND, 36.72, -4.42},
	"Jerez de la Frontera": {"Jerez de la Frontera", "Cádiz", rAND, 36.69, -6.14},
	"Córdoba":              {"Córdoba", "Córdoba", rAND, 37.89, -4.78},
	"Linares":              {"Linares", "Jaén", rAND, 38.10, -3.64},
	"Atarfe":               {"Atarfe", "Granada", rAND, 37.22, -3.69},
	"El Ejido":             {"El Ejido", "Almería", rAND, 36.78, -2.82},
	"Almería":              {"Almería", "Almería", rAND, 36.84, -2.46},
	"Zaragoza":             {"Zaragoza", "Zaragoza", rARA, 41.65, -0.89},
	"Huesca":               {"Huesca", "Huesca", rARA, 42.14, -0.41},
	"Teruel":               {"Teruel", "Teruel", rARA, 40.34, -1.11},
	"Avilés":               {"Avilés", "Asturias", rAST, 43.56, -5.93},
	"Llanera":              {"Llanera", "Asturias", rAST, 43.46, -5.87},
	"Gijón":                {"Gijón", "Asturias", rAST, 43.53, -5.66},
	"Torrelavega":          {"Torrelavega", "Cantabria", rCNB, 43.35, -4.05},
	"Santander":            {"Santander", "Cantabria", rCNB, 43.46, -3.81},
	"Valladolid":           {"Valladolid", "Valladolid", rCYL, 41.65, -4.72},
	"Burgos":               {"Burgos", "Burgos", rCYL, 42.34, -3.70},
	"Salamanca":            {"Salamanca", "Salamanca", rCYL, 40.97, -5.66},
	"Venta de Baños":       {"Venta de Baños", "Palencia", rCYL, 41.92, -4.49},
	"Albacete":             {"Albacete", "Albacete", rCLM, 38.99, -1.86},
	"Illescas":             {"Illescas", "Toledo", rCLM, 40.12, -3.85},
	"Toledo":               {"Toledo", "Toledo", rCLM, 39.86, -4.03},
	"Guadalajara":          {"Guadalajara", "Guadalajara", rCLM, 40.63, -3.17},
	"Martorell":            {"Martorell", "Barcelona", rCAT, 41.47, 1.93},
	"Navarcles":            {"Navarcles", "Barcelona", rCAT, 41.75, 1.90},
	"Montcada i Reixac":    {"Montcada i Reixac", "Barcelona", rCAT, 41.48, 2.19},
	"Constantí":            {"Constantí", "Tarragona", rCAT, 41.16, 1.21},
	"Girona":               {"Girona", "Girona", rCAT, 41.98, 2.82},
	"Lleida":               {"Lleida", "Lleida", rCAT, 41.62, 0.62},
	"Tarragona":            {"Tarragona", "Tarragona", rCAT, 41.12, 1.25},
	"Valencia":             {"Valencia", "Valencia", rVAL, 39.47, -0.38},
	"Sagunto":              {"Sagunto", "Valencia", rVAL, 39.68, -0.28},
	"Alzira":               {"Alzira", "Valencia", rVAL, 39.15, -0.44},
	"Picassent":            {"Picassent", "Valencia", rVAL, 39.36, -0.46},
	"Elche":                {"Elche", "Alicante", rVAL, 38.27, -0.70},
	"Alicante":             {"Alicante", "Alicante", rVAL, 38.35, -0.48},
	"Badajoz":              {"Badajoz", "Badajoz", rEXT, 38.88, -6.97},
	"Mérida":               {"Mérida", "Badajoz", rEXT, 38.92, -6.34},
	"Cáceres":              {"Cáceres", "Cáceres", rEXT, 39.48, -6.37},
	"A Coruña":             {"A Coruña", "A Coruña", rGAL, 43.36, -8.41},
	"Cerceda":              {"Cerceda", "A Coruña", rGAL, 43.19, -8.47},
	"Vigo":                 {"Vigo", "Pontevedra", rGAL, 42.24, -8.72},
	"Ourense":              {"Ourense", "Ourense", rGAL, 42.34, -7.86},
	"Lugo":                 {"Lugo", "Lugo", rGAL, 43.01, -7.56},
	"Getafe":               {"Getafe", "Madrid", rMAD, 40.31, -3.73},
	"Pinto":                {"Pinto", "Madrid", rMAD, 40.24, -3.70},
	"Alcalá de Henares":    {"Alcalá de Henares", "Madrid", rMAD, 40.48, -3.36},
	"Colmenar Viejo":       {"Colmenar Viejo", "Madrid", rMAD, 40.66, -3.77},
	"Murcia":               {"Murcia", "Murcia", rMUR, 37.99, -1.13},
	"Lorca":                {"Lorca", "Murcia", rMUR, 37.68, -1.70},
	"Cartagena":            {"Cartagena", "Murcia", rMUR, 37.61, -0.99},
	"Pamplona":             {"Pamplona", "Navarra", rNAV, 42.82, -1.64},
	"Tudela":               {"Tudela", "Navarra", rNAV, 42.06, -1.60},
	"Vitoria-Gasteiz":      {"Vitoria-Gasteiz", "Araba", rPV, 42.85, -2.67},
	"Bilbao":               {"Bilbao", "Bizkaia", rPV, 43.26, -2.94},
	"Amorebieta-Etxano":    {"Amorebieta-Etxano", "Bizkaia", rPV, 43.22, -2.73},
	"Urnieta":              {"Urnieta", "Gipuzkoa", rPV, 43.25, -1.99},
	"Logroño":              {"Logroño", "La Rioja", rRIO, 42.47, -2.45},
	"Calahorra":            {"Calahorra", "La Rioja", rRIO, 42.31, -1.97},
}

// actorSpec is one fictional organisation and the towns of its sites.
type actorSpec struct {
	Slug   string
	Name   string
	Kind   domain.OrgKind
	Towns  []string
	Desc   string
	Extra  string // product name (additive producers)
	Region string // home region (contractors, owners)
}

// The companies are fictional. Their names were coined for this demonstrator and are
// not meant to refer to any real business; their towns, sizes and processes follow
// public Spanish sector data (docs/research/SPAIN_DATA_SOURCES.md).
var sortingPlants = []actorSpec{
	{"sel-guadaira", "Selectiva Guadaíra S.L.", domain.KindSortingPlant, []string{"Alcalá de Guadaíra"}, "Light-packaging sorting plant serving the Seville metropolitan consortium.", "", ""},
	{"envasur-antequera", "Envasur Antequera S.A.", domain.KindSortingPlant, []string{"Antequera"}, "Sorting plant for the Málaga inland municipalities.", "", ""},
	{"clasif-jerez", "Clasificadora Campiña Jerez S.L.", domain.KindSortingPlant, []string{"Jerez de la Frontera"}, "", "", ""},
	{"ecoplanta-cordoba", "Ecoplanta Guadalquivir S.L.", domain.KindSortingPlant, []string{"Córdoba"}, "", "", ""},
	{"sel-almeria", "Seleciclo Almería S.L.", domain.KindSortingPlant, []string{"Almería"}, "", "", ""},
	{"envaselect-ebro", "Envaselect Ebro S.A.", domain.KindSortingPlant, []string{"Zaragoza"}, "Largest sorting line in the Ebro valley.", "", ""},
	{"clasif-altoaragon", "Clasificadora Altoaragón S.L.", domain.KindSortingPlant, []string{"Huesca"}, "", "", ""},
	{"sel-cantabrico", "Selectiva Cantábrica S.L.", domain.KindSortingPlant, []string{"Gijón"}, "", "", ""},
	{"ecoplanta-santander", "Ecoplanta Bahía S.L.", domain.KindSortingPlant, []string{"Santander"}, "", "", ""},
	{"envases-burgos", "Envases Castilla Norte S.L.", domain.KindSortingPlant, []string{"Burgos"}, "", "", ""},
	{"sel-tormes", "Selectiva Tormes S.L.", domain.KindSortingPlant, []string{"Salamanca"}, "", "", ""},
	{"clasif-albacete", "Clasificadora La Manchuela S.L.", domain.KindSortingPlant, []string{"Albacete"}, "", "", ""},
	{"sel-henares", "Selectiva del Henares S.A.", domain.KindSortingPlant, []string{"Guadalajara"}, "", "", ""},
	{"ecovalles-montcada", "Ecovallès Montcada S.L.", domain.KindSortingPlant, []string{"Montcada i Reixac"}, "Sorting plant for the Barcelona metropolitan area.", "", ""},
	{"clasif-camp", "Classificadora del Camp S.L.", domain.KindSortingPlant, []string{"Constantí"}, "", "", ""},
	{"sel-ribera", "Selectiva Ribera Alta S.L.", domain.KindSortingPlant, []string{"Alzira"}, "", "", ""},
	{"envases-horta", "Envases Horta Sud S.L.", domain.KindSortingPlant, []string{"Picassent"}, "", "", ""},
	{"sel-vegas", "Selectiva Vegas Bajas S.L.", domain.KindSortingPlant, []string{"Badajoz"}, "", "", ""},
	{"clasif-atlantica", "Clasificadora Atlántica S.L.", domain.KindSortingPlant, []string{"Cerceda"}, "", "", ""},
	{"sel-pinto", "Selectiva Sur Madrid S.A.", domain.KindSortingPlant, []string{"Pinto"}, "Serves the southern Madrid municipalities.", "", ""},
	{"sel-sierra", "Selectiva Sierra Norte S.L.", domain.KindSortingPlant, []string{"Colmenar Viejo"}, "", "", ""},
	{"ecoplanta-segura", "Ecoplanta Segura S.L.", domain.KindSortingPlant, []string{"Murcia"}, "", "", ""},
	{"clasif-ribera-nav", "Clasificadora Ribera Navarra S.L.", domain.KindSortingPlant, []string{"Tudela"}, "", "", ""},
	{"sel-araba", "Selektiba Araba S.L.", domain.KindSortingPlant, []string{"Vitoria-Gasteiz"}, "", "", ""},
	{"sel-durango", "Selektiba Durangaldea S.L.", domain.KindSortingPlant, []string{"Amorebieta-Etxano"}, "", "", ""},
	{"ecoplanta-rioja", "Ecoplanta Rioja S.L.", domain.KindSortingPlant, []string{"Logroño"}, "", "", ""},
}

var recyclers = []actorSpec{
	{"nuevafilm", "Nuevafilm Reciclados S.L.", domain.KindRecycler, []string{"Navarcles", "Zaragoza"}, "Washing and agglomeration of post-consumer film.", "", ""},
	{"agrofilm-renova", "Agrofilm Renova S.L.", domain.KindRecycler, []string{"El Ejido"}, "Film recycler born from agricultural greenhouse film.", "", ""},
	{"plastiverde-elx", "Plastiverde Elx S.L.", domain.KindRecycler, []string{"Elche", "Lorca"}, "", "", ""},
	{"circulen", "Circulen Cantábrico S.A.", domain.KindRecycler, []string{"Torrelavega", "Llanera"}, "", "", ""},
	{"riberfilm", "Riberfilm Reciclaje S.L.", domain.KindRecycler, []string{"Calahorra"}, "", "", ""},
	{"duero-regranza", "Duero Regranza S.L.", domain.KindRecycler, []string{"Valladolid"}, "", "", ""},
	{"linares-polimeros", "Polímeros Circulares de Linares S.L.", domain.KindRecycler, []string{"Linares"}, "", "", ""},
}

var additiveProducers = []actorSpec{
	{"asfaltix", "Asfaltix Polymers S.L.", domain.KindAdditiveProducer, []string{"Illescas", "Venta de Baños"}, "Dry-process polymer modifiers from recycled film.", "AX-Road R90", ""},
	{"vialtec", "Vialtec Aditivos S.A.", domain.KindAdditiveProducer, []string{"Sagunto"}, "Asphalt additives for Mediterranean road networks.", "VT-Plast 95", ""},
	{"ligante-circular", "Ligante Circular Ibérica S.L.", domain.KindAdditiveProducer, []string{"Martorell"}, "", "LCI Dry-Mod", ""},
}

var asphaltCompanies = []actorSpec{
	{"firmes-del-sur", "Firmes del Sur S.A.", domain.KindAsphaltPlant, []string{"Dos Hermanas", "Málaga", "Córdoba", "Atarfe", "Almería"}, "", "", rAND},
	{"aglomerados-meseta", "Aglomerados Meseta S.A.", domain.KindAsphaltPlant, []string{"Getafe", "Alcalá de Henares", "Toledo", "Salamanca", "Valladolid", "Burgos"}, "", "", rMAD},
	{"asfaltos-levantinos", "Asfaltos Levantinos S.L.", domain.KindAsphaltPlant, []string{"Valencia", "Alicante", "Murcia", "Albacete", "Cartagena"}, "", "", rVAL},
	{"mezclas-ebro", "Mezclas Bituminosas del Ebro S.L.", domain.KindAsphaltPlant, []string{"Zaragoza", "Teruel", "Logroño", "Pamplona"}, "", "", rARA},
	{"pavimentos-cantabricos", "Pavimentos Cantábricos S.A.", domain.KindAsphaltPlant, []string{"Santander", "Bilbao", "Vitoria-Gasteiz", "Avilés"}, "", "", rPV},
	{"firmes-atlanticos", "Firmes Atlánticos S.L.", domain.KindAsphaltPlant, []string{"Vigo", "A Coruña", "Ourense", "Lugo"}, "", "", rGAL},
	{"aglomerats-viaris", "Aglomerats Viaris de Catalunya S.A.", domain.KindAsphaltPlant, []string{"Montcada i Reixac", "Girona", "Tarragona", "Lleida"}, "", "", rCAT},
	{"asfaltos-extremenos", "Asfaltos Extremeños S.L.", domain.KindAsphaltPlant, []string{"Badajoz", "Cáceres", "Mérida"}, "", "", rEXT},
}

var contractors = []actorSpec{
	{"cv-guadalquivir", "Construcciones Viarias Guadalquivir S.A.", domain.KindContractor, []string{"Dos Hermanas"}, "", "", rAND},
	{"obras-centro", "Obras y Firmes del Centro S.L.", domain.KindContractor, []string{"Getafe"}, "", "", rMAD},
	{"cyl-firmes", "Firmes y Viales del Duero S.L.", domain.KindContractor, []string{"Valladolid"}, "", "", rCYL},
	{"conservacion-med", "Conservación Vial Mediterránea S.L.", domain.KindContractor, []string{"Valencia"}, "", "", rVAL},
	{"ebro-viales", "Ebro Viales S.L.", domain.KindContractor, []string{"Zaragoza"}, "", "", rARA},
	{"norte-viario", "Infraestructuras Norte Viario S.A.", domain.KindContractor, []string{"Bilbao"}, "", "", rPV},
	{"galaica-firmes", "Galaica de Firmes e Obras S.L.", domain.KindContractor, []string{"Ourense"}, "", "", rGAL},
	{"vies-valles", "Vies i Obres del Vallès S.L.", domain.KindContractor, []string{"Montcada i Reixac"}, "", "", rCAT},
	{"extremadura-vial", "Extremadura Vial S.L.", domain.KindContractor, []string{"Mérida"}, "", "", rEXT},
	{"sureste-obras", "Obras Públicas del Sureste S.L.", domain.KindContractor, []string{"Murcia"}, "", "", rMUR},
}

var roadOwners = []actorSpec{
	{"rv-sur", "Red Viaria del Sur (demo authority)", domain.KindRoadOwner, []string{"Málaga"}, "Fictional road authority for southern Spain.", "", rAND},
	{"ac-centro", "Agencia de Carreteras del Centro (demo authority)", domain.KindRoadOwner, []string{"Getafe"}, "", "", rMAD},
	{"cv-mediterraneo", "Consorcio Viario Mediterráneo (demo authority)", domain.KindRoadOwner, []string{"Valencia"}, "", "", rVAL},
	{"ev-norte", "Ente Vial del Norte (demo authority)", domain.KindRoadOwner, []string{"Bilbao"}, "", "", rPV},
	{"ag-vias", "Axencia Galega de Vías (demo authority)", domain.KindRoadOwner, []string{"A Coruña"}, "", "", rGAL},
	{"cc-ebro", "Consorcio de Carreteras del Ebro (demo authority)", domain.KindRoadOwner, []string{"Zaragoza"}, "", "", rARA},
	{"xv-catalunya", "Xarxa Viària de Catalunya (demo authority)", domain.KindRoadOwner, []string{"Girona"}, "", "", rCAT},
	{"aiv-oeste", "Agencia de Infraestructuras Viarias del Oeste (demo authority)", domain.KindRoadOwner, []string{"Mérida"}, "", "", rEXT},
}

var labs = []actorSpec{
	{"lab-materiales", "Laboratorio Ibérico de Materiales Viales S.L.", domain.KindLab, []string{"Getafe"}, "Accredited road materials laboratory (UNE-EN ISO/IEC 17025).", "", ""},
	{"ensayos-med", "Ensayos Técnicos del Mediterráneo S.L.", domain.KindLab, []string{"Valencia"}, "", "", ""},
	{"geolab-norte", "Geolab Norte S.L.", domain.KindLab, []string{"Bilbao"}, "", "", ""},
}

var oversight = []actorSpec{
	{"verifica", "Verifica Circular Auditores S.L.", domain.KindAuditor, []string{"Getafe"}, "Independent auditor of recycled-content claims.", "", ""},
	{"programme-office", "Circular Roads Programme Office (demo reporter)", domain.KindReporter, []string{"Getafe"},
		"Reads the state of the whole chain for programme reporting; submits nothing.", "", ""},
}

// Operator is the platform operator (a real company: the provider of ROADPASS).
var operatorSpec = actorSpec{"sensifai", "Sensifai BV (platform operator)", domain.KindOperator, nil, "Operates the ROADPASS platform and its blockchain ledger.", "", ""}

// roadsByRegion lists real roads per region with an approximate length (research §5).
var roadsByRegion = map[string][]struct {
	Road string
	Len  float64
}{
	rAND: {{"A-4", 630}, {"A-44", 180}, {"A-45", 160}, {"A-49", 120}, {"A-92", 380}, {"A-7", 400}, {"N-340", 450}, {"SE-30", 22}, {"SE-40", 75}},
	rMAD: {{"A-1", 100}, {"A-2", 60}, {"A-3", 60}, {"A-4", 50}, {"A-5", 60}, {"A-6", 60}, {"M-30", 33}, {"M-40", 63}, {"M-50", 85}, {"M-300", 20}},
	rCYL: {{"A-62", 360}, {"A-6", 250}, {"A-1", 180}, {"A-66", 200}},
	rCLM: {{"A-3", 150}, {"A-4", 180}, {"A-31", 120}, {"A-30", 60}},
	rVAL: {{"A-3", 100}, {"A-7", 300}, {"AP-7", 350}, {"A-31", 110}, {"V-21", 15}, {"V-30", 20}, {"CV-35", 90}, {"A-23", 70}},
	rMUR: {{"A-7", 120}, {"A-30", 110}, {"MU-30", 30}, {"RM-1", 60}},
	rARA: {{"A-2", 220}, {"A-23", 250}, {"A-68", 120}},
	rNAV: {{"A-68", 60}, {"A-15", 140}},
	rRIO: {{"A-68", 120}, {"N-232", 110}},
	rPV:  {{"A-8", 140}, {"A-1", 90}, {"N-240", 60}},
	rCNB: {{"A-8", 130}, {"A-67", 150}},
	rAST: {{"A-8", 200}, {"A-66", 80}},
	rGAL: {{"A-6", 200}, {"A-52", 300}, {"AP-9", 220}},
	rCAT: {{"A-2", 250}, {"AP-7", 300}, {"C-32", 100}, {"C-58", 50}, {"B-23", 20}, {"N-II", 300}},
	rEXT: {{"A-5", 200}, {"A-66", 280}},
}

// regionOwner maps a region to the road owner that commissions works there.
var regionOwner = map[string]string{
	rAND: "rv-sur", rMUR: "cv-mediterraneo", rVAL: "cv-mediterraneo", rMAD: "ac-centro", rCLM: "ac-centro", rCYL: "ac-centro",
	rARA: "cc-ebro", rNAV: "cc-ebro", rRIO: "cc-ebro", rPV: "ev-norte", rCNB: "ev-norte", rAST: "ev-norte",
	rGAL: "ag-vias", rCAT: "xv-catalunya", rEXT: "aiv-oeste",
}

// regionContractor maps a region to the contractors working there.
var regionContractor = map[string][]string{
	rAND: {"cv-guadalquivir"}, rMAD: {"obras-centro"}, rCLM: {"obras-centro"}, rCYL: {"cyl-firmes", "obras-centro"},
	rVAL: {"conservacion-med"}, rMUR: {"sureste-obras", "conservacion-med"}, rARA: {"ebro-viales"}, rNAV: {"ebro-viales"},
	rRIO: {"ebro-viales"}, rPV: {"norte-viario"}, rCNB: {"norte-viario"}, rAST: {"norte-viario"}, rGAL: {"galaica-firmes"},
	rCAT: {"vies-valles"}, rEXT: {"extremadura-vial"},
}

// incinerationShare is the share of reject lots whose avoided route is energy
// recovery rather than landfill, by region (landfill dominates in most of Spain;
// Catalonia, the Basque Country, Cantabria and Mallorca run waste-to-energy plants).
var incinerationShare = map[string]float64{rCAT: 0.45, rPV: 0.55, rCNB: 0.4, rGAL: 0.35, rMAD: 0.1}
