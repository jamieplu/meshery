package mesh

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/layer5io/meshery/mesheryctl/internal/cli/root/config"
	"github.com/layer5io/meshery/mesheryctl/pkg/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Operation is the common body type to be passed for Mesh Ops
type Operation struct {
	Adapter    string `json:"adapter"`
	CustomBody string `json:"customBody"`
	DeleteOp   string `json:"deleteOp"`
	Namespace  string `json:"namespace"`
	Query      string `json:"query"`
}

var spec string

// validateCmd represents the service mesh validation command
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate conformance to service mesh standards",
	Example: `
// Validate conformance to service mesh standards
mesheryctl mesh validate [mesh name] --adapter [name of the adapter] --tokenPath [path to token for authentication] --spec [specification to be used for conformance test] --namespace [namespace to be used]

// Validate Istio to service mesh standards
mesheryctl mesh validate istio --adapter meshery-istio --spec smi

! Refer below image link for usage
* Usage of mesheryctl mesh validate
# ![mesh-validate-usage](/assets/img/mesheryctl/mesh-validate.png)
	`,
	Long: `Validate service mesh conformance to different standard specifications`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		log.Infof("Verifying prerequisites...")

		mctlCfg, err := config.GetMesheryCtl(viper.GetViper())
		if err != nil {
			log.Fatalln(err)
		}

		prefs, err := utils.GetSessionData(mctlCfg)
		if err != nil {
			log.Fatalln(err)
		}
		//resolve adapterUrl to adapter Location
		for _, adapter := range prefs.MeshAdapters {
			adapterName := strings.Split(adapter.Location, ":")
			if adapterName[0] == adapterURL {
				adapterURL = adapter.Location
				meshName = adapter.Location
			}
		}
		//sync with available adapters
		if err = validateAdapter(mctlCfg, meshName); err != nil {
			log.Fatalln(err)
		}
		log.Info("verified prerequisites")
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Infof("Starting service mesh validation...")

		mctlCfg, err := config.GetMesheryCtl(viper.GetViper())
		if err != nil {
			log.Fatalln(err)
		}
		s := utils.CreateDefaultSpinner(fmt.Sprintf("Validating %s", meshName), fmt.Sprintf("\n%s validation successful", meshName))
		s.Start()
		_, err = sendValidateRequest(mctlCfg, meshName, false)
		if err != nil {
			log.Fatalln(err)
		}
		s.Stop()

		if watch {
			log.Infof("Verifying Operation")
			_, err = waitForValidateResponse(mctlCfg, "Smi conformance test")
			if err != nil {
				log.Fatalln(err)
			}
		}

		return nil
	},
}

func init() {
	validateCmd.Flags().StringVarP(&spec, "spec", "s", "smi", "(Required) specification to be used for conformance test")
	_ = validateCmd.MarkFlagRequired("spec")
	validateCmd.Flags().StringVarP(&adapterURL, "adapter", "a", "meshery-osm", "(Required) Adapter to use for validation")
	_ = validateCmd.MarkFlagRequired("adapter")
	validateCmd.Flags().StringVarP(&utils.TokenFlag, "token", "t", "", "Path to token for authenticating to Meshery API")
	validateCmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch for events and verify operation (in beta testing)")
}

func waitForValidateResponse(mctlCfg *config.MesheryCtlConfig, query string) (string, error) {
	path := mctlCfg.GetBaseMesheryURL() + "/api/events?client=cli_validate"
	method := "GET"
	client := &http.Client{}
	req, err := utils.NewRequest(method, path, nil)
	req.Header.Add("Accept", "text/event-stream")
	if err != nil {
		return "", ErrCreatingDeployResponseRequest(err)
	}

	res, err := client.Do(req)
	if err != nil {
		return "", ErrCreatingValidateRequest(err)
	}

	event, err := utils.ConvertRespToSSE(res)
	if err != nil {
		return "", ErrCreatingValidateResponseStream(err)
	}

	timer := time.NewTimer(time.Duration(1200) * time.Second)
	eventChan := make(chan string)

	//Run a goroutine to wait for the response
	go func() {
		for i := range event {
			if strings.Contains(i.Data.Summary, query) {
				eventChan <- "successful"
				log.Infof("%s\n%s", i.Data.Summary, i.Data.Details)
			} else if strings.Contains(i.Data.Details, "error") {
				eventChan <- "error"
				log.Infof("%s", i.Data.Summary)
			}
		}
	}()

	select {
	case <-timer.C:
		return "", ErrTimeoutWaitingForValidateResponse
	case event := <-eventChan:
		if event != "successful" {
			return "", ErrSMIConformanceTestsFailed
		}
	}

	return "", nil
}
