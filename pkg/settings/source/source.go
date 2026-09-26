package source

import (
	"github.com/fourleggedlabs/dinghy/pkg/settings/global"
	"github.com/fourleggedlabs/dinghy/pkg/util"
	logr "github.com/sirupsen/logrus"
	"net/http"
)

//go:generate mockgen -destination=source_configuration_mock.go -package source -source source.go

type SourceConfiguration interface {
	GetSourceName() string
	LoadSetupSettings(*logr.Logger) (*global.Settings, error)
	GetSettings(r *http.Request, logr *logr.Logger) (*global.Settings, util.PlankClient, error)
	BustCacheHandler(w http.ResponseWriter, r *http.Request)
	IsMultiTenant() bool
}
