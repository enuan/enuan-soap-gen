package tagliandi

import (
	"encoding/xml"
)

// Simple types

// LoginStateCode represents an enumeration type
type LoginStateCode string

// LoginStateCode enumeration values
const (
	LoginStateCodeSUCCESS             LoginStateCode = "SUCCESS"
	LoginStateCodeSESSIONERROR        LoginStateCode = "SESSIONERROR"
	LoginStateCodePOLICYERROR         LoginStateCode = "POLICYERROR"
	LoginStateCodeAUTHENTICATIONERROR LoginStateCode = "AUTHENTICATIONERROR"
)

// String returns the string representation of LoginStateCode
func (e LoginStateCode) String() string {
	return string(e)
}

// IsValid returns true if the LoginStateCode value is valid
func (e LoginStateCode) IsValid() bool {
	switch e {
	case LoginStateCodeSUCCESS, LoginStateCodeSESSIONERROR, LoginStateCodePOLICYERROR, LoginStateCodeAUTHENTICATIONERROR:
		return true
	default:
		return false
	}
}

// Complex types

// ArrayOfExtraTime represents the ArrayOfExtraTime complex type
type ArrayOfExtraTime struct {
	ExtraTime []ExtraTime `xml:"ExtraTime,omitempty"`
}

// ArrayOfInformazioniTecniche represents the ArrayOfInformazioniTecniche complex type
type ArrayOfInformazioniTecniche struct {
	InformazioniTecniche []InformazioniTecniche `xml:"InformazioniTecniche,omitempty"`
}

// ArrayOfInt represents the ArrayOfInt complex type
type ArrayOfInt struct {
	Int []int32 `xml:"int,omitempty"`
}

// ArrayOfIntervalliTagliando represents the ArrayOfIntervalliTagliando complex type
type ArrayOfIntervalliTagliando struct {
	IntervalliTagliando []IntervalliTagliando `xml:"IntervalliTagliando,omitempty"`
}

// ArrayOfOperazione represents the ArrayOfOperazione complex type
type ArrayOfOperazione struct {
	Operazione []Operazione `xml:"Operazione,omitempty"`
}

// ArrayOfPianoManutentivo represents the ArrayOfPianoManutentivo complex type
type ArrayOfPianoManutentivo struct {
	PianoManutentivo []PianoManutentivo `xml:"PianoManutentivo,omitempty"`
}

// ArrayOfPianoManutentivoModello represents the ArrayOfPianoManutentivoModello complex type
type ArrayOfPianoManutentivoModello struct {
	PianoManutentivoModello []PianoManutentivoModello `xml:"PianoManutentivoModello,omitempty"`
}

// ArrayOfRicambioDBRT represents the ArrayOfRicambioDBRT complex type
type ArrayOfRicambioDBRT struct {
	RicambioDBRT []RicambioDBRT `xml:"RicambioDBRT,omitempty"`
}

// ArrayOfRicambioTagliando represents the ArrayOfRicambioTagliando complex type
type ArrayOfRicambioTagliando struct {
	RicambioTagliando []RicambioTagliando `xml:"RicambioTagliando,omitempty"`
}

// ArrayOfString represents the ArrayOfString complex type
type ArrayOfString struct {
	String []string `xml:"string,omitempty"`
}

// ArrayOfTipoTagliando represents the ArrayOfTipoTagliando complex type
type ArrayOfTipoTagliando struct {
	TipoTagliando []TipoTagliando `xml:"TipoTagliando,omitempty"`
}

// ArrayOfVarianteListino represents the ArrayOfVarianteListino complex type
type ArrayOfVarianteListino struct {
	VarianteListino []VarianteListino `xml:"VarianteListino,omitempty"`
}

// BaseContext represents the BaseContext complex type
type BaseContext struct {
	SessionId           *string              `xml:"SessionId,omitempty"`
	TagliandoParameters *TagliandoParameters `xml:"tagliandoParameters,omitempty"`
	VeicoloParameters   *VeicoloParameters   `xml:"veicoloParameters,omitempty"`
}

// BaseRequest represents the BaseRequest complex type
type BaseRequest struct {
	SessionId *string `xml:"SessionId,omitempty"`
}

// ExtraTime represents the ExtraTime complex type
type ExtraTime struct {
	Selezionato bool    `xml:"Selezionato"`
	Id          int32   `xml:"Id"`
	Descrizione *string `xml:"Descrizione,omitempty"`
	Tempo       float64 `xml:"Tempo"`
}

// InformazioniTecniche represents the InformazioniTecniche complex type
type InformazioniTecniche struct {
	Id          int32   `xml:"Id"`
	TextLeft    *string `xml:"TextLeft,omitempty"`
	TextRight   *string `xml:"TextRight,omitempty"`
	Descrizione *string `xml:"Descrizione,omitempty"`
	NoteRef     *string `xml:"NoteRef,omitempty"`
	GRef        *string `xml:"GRef,omitempty"`
}

// IntervalliTagliando represents the IntervalliTagliando complex type
type IntervalliTagliando struct {
	Id                      int32             `xml:"Id"`
	Selezionato             bool              `xml:"Selezionato"`
	Sequenza                int32             `xml:"Sequenza"`
	Variante                *string           `xml:"Variante,omitempty"`
	Descrizione             *string           `xml:"Descrizione,omitempty"`
	Km                      int32             `xml:"Km"`
	Mesi                    int32             `xml:"Mesi"`
	MesiPianificati         int32             `xml:"MesiPianificati"`
	TempoStandard           float64           `xml:"TempoStandard"`
	TempoStandardAutomatico float64           `xml:"TempoStandardAutomatico"`
	DifferenzaPercentuale   float64           `xml:"DifferenzaPercentuale"`
	Icon                    *string           `xml:"Icon,omitempty"`
	ExtraTimeCollection     *ArrayOfExtraTime `xml:"ExtraTimeCollection,omitempty"`
}

// IntervalsResult represents the IntervalsResult complex type
type IntervalsResult struct {
	Successo                      bool                        `xml:"Successo"`
	CodiceErrore                  int32                       `xml:"CodiceErrore"`
	Messaggio                     *string                     `xml:"Messaggio,omitempty"`
	IntervalliTagliandoCollection *ArrayOfIntervalliTagliando `xml:"IntervalliTagliandoCollection,omitempty"`
	TipoTagliando                 *string                     `xml:"TipoTagliando,omitempty"`
	IdTagliandoSelezionato        int32                       `xml:"IdTagliandoSelezionato"`
}

// LoginRequest represents the LoginRequest complex type
type LoginRequest struct {
	CompanyLogin     *string `xml:"CompanyLogin,omitempty"`
	Password         *string `xml:"Password,omitempty"`
	ClientKey        *string `xml:"ClientKey,omitempty"`
	ProductName      *string `xml:"ProductName,omitempty"`
	ServPartsCompany *string `xml:"ServPartsCompany,omitempty"`
}

// LoginSeviceResult represents the LoginSeviceResult complex type
type LoginSeviceResult struct {
	SessionId  *string        `xml:"SessionId,omitempty"`
	LoginState LoginStateCode `xml:"LoginState"`
	ErrorMsg   *string        `xml:"ErrorMsg,omitempty"`
}

// OperationSparePartsResult represents the OperationSparePartsResult complex type
type OperationSparePartsResult struct {
	Successo                    bool                      `xml:"Successo"`
	CodiceErrore                int32                     `xml:"CodiceErrore"`
	Messaggio                   *string                   `xml:"Messaggio,omitempty"`
	RicambioTagliandoCollection *ArrayOfRicambioTagliando `xml:"RicambioTagliandoCollection,omitempty"`
	OperazioneCollection        *ArrayOfOperazione        `xml:"OperazioneCollection,omitempty"`
}

// Operazione represents the Operazione complex type
type Operazione struct {
	IdLinea                    int32   `xml:"IdLinea"`
	OperazioneRiferimento      *string `xml:"OperazioneRiferimento,omitempty"`
	OperazioneDescrizione      *string `xml:"OperazioneDescrizione,omitempty"`
	OperazioneTipo             *string `xml:"OperazioneTipo,omitempty"`
	NoteRiferimentoId          int32   `xml:"NoteRiferimentoId"`
	NoteTitolo                 *string `xml:"NoteTitolo,omitempty"`
	NoteRiferimentoDescrizione *string `xml:"NoteRiferimentoDescrizione,omitempty"`
}

// PianoManutentivo represents the PianoManutentivo complex type
type PianoManutentivo struct {
	MidId                             *string                         `xml:"MidId,omitempty"`
	PianoManutentivoModelloCollection *ArrayOfPianoManutentivoModello `xml:"PianoManutentivoModelloCollection,omitempty"`
}

// PianoManutentivoBase represents the PianoManutentivoBase complex type
type PianoManutentivoBase struct {
	MidId *string `xml:"MidId,omitempty"`
}

// PianoManutentivoModello represents the PianoManutentivoModello complex type
type PianoManutentivoModello struct {
	Id                      int64                 `xml:"Id"`
	Selezionato             bool                  `xml:"Selezionato"`
	NomeModello             *string               `xml:"NomeModello,omitempty"`
	Carburante              *string               `xml:"Carburante,omitempty"`
	Info                    *string               `xml:"Info,omitempty"`
	CodiceMotore            *string               `xml:"CodiceMotore,omitempty"`
	Potenza                 *string               `xml:"Potenza,omitempty"`
	UlterioreDescrizione    *string               `xml:"UlterioreDescrizione,omitempty"`
	InizioAnnoProduzione    *string               `xml:"InizioAnnoProduzione,omitempty"`
	FineAnnoProduzione      *string               `xml:"FineAnnoProduzione,omitempty"`
	TipoVeicolo             int32                 `xml:"TipoVeicolo"`
	TipoTagliandoCollection *ArrayOfTipoTagliando `xml:"TipoTagliandoCollection,omitempty"`
}

// PianoManutentivoModelloBase1 represents the PianoManutentivoModelloBase1 complex type
type PianoManutentivoModelloBase1 struct {
	Id                   int64   `xml:"Id"`
	Selezionato          bool    `xml:"Selezionato"`
	NomeModello          *string `xml:"NomeModello,omitempty"`
	Carburante           *string `xml:"Carburante,omitempty"`
	Info                 *string `xml:"Info,omitempty"`
	CodiceMotore         *string `xml:"CodiceMotore,omitempty"`
	Potenza              *string `xml:"Potenza,omitempty"`
	UlterioreDescrizione *string `xml:"UlterioreDescrizione,omitempty"`
	InizioAnnoProduzione *string `xml:"InizioAnnoProduzione,omitempty"`
	FineAnnoProduzione   *string `xml:"FineAnnoProduzione,omitempty"`
	TipoVeicolo          int32   `xml:"TipoVeicolo"`
}

// ResultBase represents the ResultBase complex type
type ResultBase struct {
	Successo     bool    `xml:"Successo"`
	CodiceErrore int32   `xml:"CodiceErrore"`
	Messaggio    *string `xml:"Messaggio,omitempty"`
}

// RicambioDBRT represents the RicambioDBRT complex type
type RicambioDBRT struct {
	IdPar                     int32                   `xml:"IdPar"`
	IdSimmetria               *string                 `xml:"IdSimmetria,omitempty"`
	TipoPar                   *string                 `xml:"TipoPar,omitempty"`
	VariantiListinoCollection *ArrayOfVarianteListino `xml:"VariantiListinoCollection,omitempty"`
}

// RicambioTagliando represents the RicambioTagliando complex type
type RicambioTagliando struct {
	IdLinea                        int32                        `xml:"IdLinea"`
	IdPars                         *ArrayOfInt                  `xml:"IdPars,omitempty"`
	Selezionato                    bool                         `xml:"Selezionato"`
	OperazioneRiferimento          *string                      `xml:"OperazioneRiferimento,omitempty"`
	Quantita                       *string                      `xml:"Quantita,omitempty"`
	Descrizione                    *string                      `xml:"Descrizione,omitempty"`
	IdParCollection                *ArrayOfString               `xml:"IdParCollection,omitempty"`
	Tempo                          float64                      `xml:"Tempo"`
	ItemId                         *string                      `xml:"ItemId,omitempty"`
	CostiAggiuntivi                bool                         `xml:"CostiAggiuntivi"`
	Note                           *string                      `xml:"Note,omitempty"`
	InformazioniTecnicheCollection *ArrayOfInformazioniTecniche `xml:"InformazioniTecnicheCollection,omitempty"`
	TempiAggiuntiviCollection      *ArrayOfInformazioniTecniche `xml:"TempiAggiuntiviCollection,omitempty"`
	BDRTDatiRicambioCollection     *ArrayOfRicambioDBRT         `xml:"BDRTDatiRicambioCollection,omitempty"`
}

// TagliandoParameters represents the TagliandoParameters complex type
type TagliandoParameters struct {
	Id       int32       `xml:"Id"`
	LinkId   *string     `xml:"LinkId,omitempty"`
	Sequenza *ArrayOfInt `xml:"Sequenza,omitempty"`
}

// TipoTagliando represents the TipoTagliando complex type
type TipoTagliando struct {
	Id                           int64   `xml:"Id"`
	Costruttore                  *string `xml:"Costruttore,omitempty"`
	IdCategoria                  *string `xml:"IdCategoria,omitempty"`
	TipoCategoria                int32   `xml:"TipoCategoria"`
	Variante                     int32   `xml:"Variante"`
	LinkId                       *string `xml:"LinkId,omitempty"`
	Selezionato                  bool    `xml:"Selezionato"`
	DescrizioneVariante          *string `xml:"DescrizioneVariante,omitempty"`
	DescrizioneVarianteOriginale *string `xml:"DescrizioneVarianteOriginale,omitempty"`
}

// Type1 represents the Type1 complex type
type Type1 struct {
	SessionId         *string                     `xml:"SessionId,omitempty"`
	VeicoloParameters *VeicoloParametersWithPlate `xml:"veicoloParameters,omitempty"`
}

// Type2 represents the Type2 complex type
type Type2 struct {
	Successo                   bool                     `xml:"Successo"`
	CodiceErrore               int32                    `xml:"CodiceErrore"`
	Messaggio                  *string                  `xml:"Messaggio,omitempty"`
	CodiceMotore               *string                  `xml:"CodiceMotore,omitempty"`
	DataPrimaImmatricolazione  *string                  `xml:"DataPrimaImmatricolazione,omitempty"`
	DataUltimaRevisione        *string                  `xml:"DataUltimaRevisione,omitempty"`
	Element1                   *string                  `xml:"Element1,omitempty"`
	Element5                   *ArrayOfString           `xml:"Element5,omitempty"`
	Telaio                     *string                  `xml:"Telaio,omitempty"`
	PianoManutentivoCollection *ArrayOfPianoManutentivo `xml:"PianoManutentivoCollection,omitempty"`
}

// VarianteListino represents the VarianteListino complex type
type VarianteListino struct {
	Colore               *string `xml:"Colore,omitempty"`
	Descrizione          *string `xml:"Descrizione,omitempty"`
	ParNumber            *string `xml:"ParNumber,omitempty"`
	PeriodoCostruttivo   *string `xml:"PeriodoCostruttivo,omitempty"`
	PrezzoListino        float64 `xml:"PrezzoListino"`
	UlterioreDescrizione *string `xml:"UlterioreDescrizione,omitempty"`
}

// VeicoloParameters represents the VeicoloParameters complex type
type VeicoloParameters struct {
	Element1             *string `xml:"Element1,omitempty"`
	CodiceMotore         *string `xml:"CodiceMotore,omitempty"`
	KmPercorsi           int32   `xml:"KmPercorsi"`
	DataImmatricolazione *string `xml:"DataImmatricolazione,omitempty"`
	DataUltimaRevisione  *string `xml:"DataUltimaRevisione,omitempty"`
	Telaio               *string `xml:"Telaio,omitempty"`
}

// VeicoloParametersWithPlate represents the VeicoloParametersWithPlate complex type
type VeicoloParametersWithPlate struct {
	Targa                *string `xml:"Targa,omitempty"`
	Element1             *string `xml:"Element1,omitempty"`
	CodiceMotore         *string `xml:"CodiceMotore,omitempty"`
	KmPercorsi           int32   `xml:"KmPercorsi"`
	DataImmatricolazione *string `xml:"DataImmatricolazione,omitempty"`
	DataUltimaRevisione  *string `xml:"DataUltimaRevisione,omitempty"`
	Telaio               *string `xml:"Telaio,omitempty"`
}

// LoginWrapper represents the Login element
type LoginWrapper struct {
	XMLName xml.Name      `xml:"urn:example:ns1 Login"`
	Request *LoginRequest `xml:"request,omitempty"`
}

// LoginResponseWrapper represents the LoginResponse element
type LoginResponseWrapper struct {
	XMLName     xml.Name           `xml:"urn:example:ns1 LoginResponse"`
	LoginResult *LoginSeviceResult `xml:"LoginResult,omitempty"`
}

// GetOperationSparePartsWrapper represents the GetOperationSpareParts element
type GetOperationSparePartsWrapper struct {
	XMLName xml.Name     `xml:"urn:example:ns1 GetOperationSpareParts"`
	Context *BaseContext `xml:"context,omitempty"`
}

// GetOperationSparePartsResponseWrapper represents the GetOperationSparePartsResponse element
type GetOperationSparePartsResponseWrapper struct {
	XMLName                      xml.Name                   `xml:"urn:example:ns1 GetOperationSparePartsResponse"`
	GetOperationSparePartsResult *OperationSparePartsResult `xml:"GetOperationSparePartsResult,omitempty"`
}

// GetIntervalsWrapper represents the GetIntervals element
type GetIntervalsWrapper struct {
	XMLName xml.Name     `xml:"urn:example:ns1 GetIntervals"`
	Context *BaseContext `xml:"context,omitempty"`
}

// GetIntervalsResponseWrapper represents the GetIntervalsResponse element
type GetIntervalsResponseWrapper struct {
	XMLName            xml.Name         `xml:"urn:example:ns1 GetIntervalsResponse"`
	GetIntervalsResult *IntervalsResult `xml:"GetIntervalsResult,omitempty"`
}

// GetIntervalsAdditionalWrapper represents the GetIntervalsAdditional element
type GetIntervalsAdditionalWrapper struct {
	XMLName xml.Name     `xml:"urn:example:ns1 GetIntervalsAdditional"`
	Context *BaseContext `xml:"context,omitempty"`
}

// GetIntervalsAdditionalResponseWrapper represents the GetIntervalsAdditionalResponse element
type GetIntervalsAdditionalResponseWrapper struct {
	XMLName                      xml.Name         `xml:"urn:example:ns1 GetIntervalsAdditionalResponse"`
	GetIntervalsAdditionalResult *IntervalsResult `xml:"GetIntervalsAdditionalResult,omitempty"`
}

// Element2Wrapper represents the Element2 element
type Element2Wrapper struct {
	XMLName xml.Name `xml:"urn:example:ns1 Element2"`
	Context *Type1   `xml:"context,omitempty"`
}

// Element3Wrapper represents the Element3 element
type Element3Wrapper struct {
	XMLName  xml.Name `xml:"urn:example:ns1 Element3"`
	Element4 *Type2   `xml:"Element4,omitempty"`
}
