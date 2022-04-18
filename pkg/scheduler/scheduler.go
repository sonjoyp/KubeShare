package scheduler

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	// prometheus
	"github.com/prometheus/client_golang/api"
	promeV1 "github.com/prometheus/client_golang/api/prometheus/v1"

	// KubeShare
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	framework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
	schedulernodeinfo "k8s.io/kubernetes/pkg/scheduler/nodeinfo"

	"KubeShare/pkg/logger"
	"KubeShare/pkg/signals"
)

const (
	// the name of the plugin used in Registry and configurations
	Name = "kubeshare-scheduler"

	// the file storing the log of kubeshare scheduler
	logPath = "kubeshare-scheduler.log"
	// the file storing physical gpu position
	configPath = "/kubeshare/scheduler/kubeshare-config.yaml"
)

var (
	_ framework.QueueSortPlugin = &KubeShareScheduler{}
	_ framework.PreFilterPlugin = &KubeShareScheduler{}
	_ framework.FilterPlugin    = &KubeShareScheduler{}
	_ framework.ReservePlugin   = &KubeShareScheduler{}
	_ framework.UnreservePlugin = &KubeShareScheduler{}
	_ framework.ScorePlugin     = &KubeShareScheduler{}
)

type Args struct {
	// kubernetes
	masterURL  string `json:"master,omitempty"`
	kubeConfig string `json:"kubeconfig,omitempty"`

	// prometheus
	prometheusURL string `json:"prometheusURL,omitempty"`

	// gpu topology configration
	kubeShareConfig string `json:"kubeShareConfig,omitempty"`

	// logger
	level int64 `json:"level,omitempty"`
}

type KubeShareScheduler struct {
	// parameters of scheduler
	args     *Args
	handle   framework.FrameworkHandle
	promeAPI promeV1.API
	ksl      *logrus.Logger

	// allocation
	gpuPriority       map[string]int32
	sortGPUByPriority []string
	cellFreeList      map[string]LevelCellList
	cellElements      map[string]*cellElement
	cellMutex         *sync.RWMutex
}

// initializes a new plugin and returns it
func New(config *runtime.Unknown, handle framework.FrameworkHandle) (framework.Plugin, error) {

	// defaulting argument
	args := &Args{
		level:           3, // the default level is debugging mode
		prometheusURL:   "http://prometheus-k8s.monitoring:9090",
		kubeShareConfig: configPath,
	}
	// parse flag
	if err := framework.DecodeInto(config, args); err != nil {
		return nil, err
	}

	// logger
	ksl := logger.New(args.level, logPath)

	// prometheus
	client, err := api.NewClient(api.Config{
		Address: *&args.prometheusURL,
	})
	if err != nil {
		ksl.Printf("Error creating client: %v\n", err)
		os.Exit(1)
	}

	promeAPI := promeV1.NewAPI(client)

	kss := &KubeShareScheduler{
		args:        args,
		handle:      handle,
		promeAPI:    promeAPI,
		gpuPriority: map[string]int32{},
		cellMutex:   &sync.RWMutex{},
		ksl:         ksl,
	}
	// gpu topology
	kubeshareConfig := kss.initRawConfig()
	// ksl.Debugln("=================READ CONFIG=================")
	// ksl.Debugf("%+v", kubeshareConfig)
	kss.watchConfig(kubeshareConfig)

	// ksl.Debugln("=================CELL ELEMENTS=================")
	ce := kss.buildCellChains(kubeshareConfig.CellTypes)
	// for k, v := range ce {
	// 	ksl.Debugf("%+v = %+v", k, v)
	// }
	// ksl.Debugln("=================FREE CELL=================")
	cellFreeList := newCellConstructor(ce, kubeshareConfig.Cells, ksl).build()
	// for k, v := range cellFreeList {
	// 	ksl.Debugf("%+v = ", k)
	// 	for l, cl := range v {
	// 		for i := range cl {
	// 			ksl.Debugf("%+v = %+v", l, cl[i])
	// 		}
	// 	}
	// }

	ksl.Debugln("=================FREE CELL=================")
	ksl.Debugf("size of Free cell: %v", len(cellFreeList))
	for k, v := range cellFreeList {
		ksl.Debugf("%+v = %+v", k, v)
	}

	kss.cellElements = ce
	kss.cellFreeList = cellFreeList

	// try to comment the following two command before run TestPermit
	stopCh := signals.SetupSignalHandler()

	nodeInformer := handle.SharedInformerFactory().Core().V1().Nodes().Informer()
	nodeInformer.AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: kss.isGPUNode,
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc: kss.addNode,
				// UpdateFunc: kss.updateNode,
				// DeleteFunc: kss.deleteNode,
			},
		},
	)

	go nodeInformer.Run(stopCh)

	if !cache.WaitForCacheSync(
		stopCh,
		nodeInformer.HasSynced) {
		//podInformer.HasSynced) {
		panic(fmt.Errorf("Failed to WaitForCacheSync"))
	}

	return kss, nil
}

func (kss *KubeShareScheduler) Name() string {
	return Name
}

// sort pods in the scheduling queue.
func (kss *KubeShareScheduler) Less(podInfo1, podInfo2 *framework.PodInfo) bool {
	kss.ksl.Debugf("[QueueSort] pod1: %v/%v(%v) v.s. pod2: %v/%v(%v)", podInfo1.Pod.Namespace, podInfo1.Pod.Name, podInfo1.Pod.UID, podInfo2.Pod.Namespace, podInfo2.Pod.Name, podInfo2.Pod.UID)
	return true
}

func (kss *KubeShareScheduler) PreFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod) *framework.Status {
	kss.ksl.Infof("[PreFilter] pod: %v/%v(%v) in node %v", pod.Namespace, pod.Name, pod.UID, pod.Spec.NodeName)
	return framework.NewStatus(framework.Success, "")
}

func (kss *KubeShareScheduler) PreFilterExtensions() framework.PreFilterExtensions {
	kss.ksl.Infof("[PreFilterExtensions]")
	return nil
}

func (kss *KubeShareScheduler) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, node *schedulernodeinfo.NodeInfo) *framework.Status {
	kss.ksl.Infof("[Filter] pod: %v/%v(%v) in node %v", pod.Namespace, pod.Name, pod.UID, pod.Spec.NodeName)
	return framework.NewStatus(framework.Success, "")
}

func (kss *KubeShareScheduler) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	kss.ksl.Infof("[Score] pod: %v/%v(%v) in node %v", pod.Namespace, pod.Name, pod.UID, nodeName)
	return 0, framework.NewStatus(framework.Success, "")
}

func (kss *KubeShareScheduler) ScoreExtensions() framework.ScoreExtensions {
	kss.ksl.Infof("[ScoreExtensions]")
	return nil
}

func (kss *KubeShareScheduler) Reserve(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) *framework.Status {
	kss.ksl.Infof("[Reserve] pod: %v/%v(%v) in node %v", pod.Namespace, pod.Name, pod.UID, pod.Spec.NodeName)
	return framework.NewStatus(framework.Success, "")
}

func (kss *KubeShareScheduler) Unreserve(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {
	kss.ksl.Infof("[UnReserve] pod: %v/%v(%v) in node %v", pod.Namespace, pod.Name, pod.UID, pod.Spec.NodeName)
}

func (kss *KubeShareScheduler) Permit(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (*framework.Status, time.Duration) {
	kss.ksl.Infof("[Permit] pod: %v/%v(%v) in node %v", pod.Namespace, pod.Name, pod.UID, pod.Spec.NodeName)
	return framework.NewStatus(framework.Success, ""), 0
}
