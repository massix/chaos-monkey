package watcher

import (
	"context"
	"errors"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	apicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

type PodWatcher struct {
	corev1.PodInterface
	record.EventRecorderLogger

	Mutex         *sync.Mutex
	Namespace     string
	LabelSelector string
	PodList       []*apicorev1.Pod
	Timeout       time.Duration
	Enabled       bool
	Running       bool
}

var _ = (ConfigurableWatcher)((*PodWatcher)(nil))

func NewPodWatcher(clientset kubernetes.Interface, recorder record.EventRecorderLogger, namespace string, labelSelector ...string) ConfigurableWatcher {
	logrus.Infof("Creating pod watcher in namespace %s for label selector %s", namespace, labelSelector)

	if recorder == nil {
		logrus.Debugf("No recorder provided, using default")
		bc := record.NewBroadcaster()
		bc.StartRecordingToSink(&corev1.EventSinkImpl{
			Interface: clientset.CoreV1().Events(namespace),
		})
		recorder = bc.NewRecorder(scheme.Scheme, apicorev1.EventSource{Component: "chaos-monkey"})
	}

	return &PodWatcher{
		PodInterface:        clientset.CoreV1().Pods(namespace),
		EventRecorderLogger: recorder,

		Mutex:         &sync.Mutex{},
		Namespace:     namespace,
		LabelSelector: strings.Join(labelSelector, ","),
		PodList:       []*apicorev1.Pod{},
		Timeout:       30 * time.Second,
		Enabled:       true,
		Running:       false,
	}
}

// SetTimeout implements ConfigurableWatcher.
func (p *PodWatcher) SetTimeout(v time.Duration) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	logrus.Debugf("Setting new timeout: %s, old timeout: %s", v, p.Timeout)
	p.Timeout = v
}

// SetEnabled implements ConfigurableWatcher.
func (p *PodWatcher) SetEnabled(v bool) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	p.Enabled = v
}

// SetMaxReplicas implements ConfigurableWatcher.
func (p *PodWatcher) SetMaxReplicas(v int) {
	// No op
}

// SetMinReplicas implements ConfigurableWatcher.
func (p *PodWatcher) SetMinReplicas(v int) {
	// No op
}

// IsRunning implements Watcher.
func (p *PodWatcher) IsRunning() bool {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	return p.Running
}

// Start implements Watcher.
func (p *PodWatcher) Start(ctx context.Context) error {
	var err error
	logrus.Infof("Starting pod watcher in namespace %s", p.Namespace)
	w, err := p.Watch(ctx, metav1.ListOptions{
		Watch:         true,
		LabelSelector: p.LabelSelector,
	})
	if err != nil {
		return err
	}

	eventFormat := "Pod may be targeted by next Chaos Monkey in %s"

	defer w.Stop()

	p.setRunning(true)
	timer := time.NewTimer(p.getTimeout())

	for p.IsRunning() {
		select {
		case evt, ok := <-w.ResultChan():
			logrus.Debugf("Pod Watcher received event: %s", evt.Type)
			pod := evt.Object.(*apicorev1.Pod)

			if !ok {
				return errors.New("Pod Watcher channel closed")
			}

			switch evt.Type {
			case "", watch.Error:
				logrus.Errorf("Received empty event or error from pod watcher: %+v", evt)
				err = errors.New("Empty event or error from pod watcher")
				_ = p.Stop()
			case watch.Added:
				logrus.Infof("Adding pod to list: %s", pod.Name)
				p.addPodToList(pod)
				p.Eventf(pod, apicorev1.EventTypeNormal, "ChaosMonkeyTarget", eventFormat, p.getTimeout())
				logrus.Debug("Pod added, event sent!")
			case watch.Deleted:
				logrus.Infof("Removing pod from list: %s", pod.Name)
				p.removePodFromList(pod)
			case watch.Modified:
				logrus.Debugf("Ignoring modification of pod %s", pod.Name)
			}
		case <-ctx.Done():
			logrus.Info("Pod Watcher context done")
			err = p.Stop()
		case <-timer.C:
			if !p.isEnabled() {
				logrus.Debug("CRD not enabled, refusing to disrupt pods")
				timer.Reset(p.getTimeout())
				continue
			}

			logrus.Infof("Disrupting random pod from namespace %s", p.Namespace)
			if randomPod, err := p.getRandomPod(); err != nil {
				logrus.Warnf("Warning: %s", err)
			} else {
				logrus.Infof("Disrupting pod %s", randomPod.Name)
				gracePeriod := int64(0)
				if err := p.Delete(ctx, randomPod.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod}); err != nil {
					logrus.Errorf("Could not disrupt pod: %s", err)
				} else {
					p.Event(
						randomPod,
						apicorev1.EventTypeNormal,
						"ChaosMonkeyTarget",
						"Pod got killed by Chaos Monkey",
					)
					logrus.Debug("Pod disrupted, event sent!")
				}
			}

			p.Mutex.Lock()
			// Send an event to all the Pods, they could be the next target!
			for _, pod := range p.PodList {
				p.Eventf(pod, apicorev1.EventTypeNormal, "ChaosMonkeyTarget", eventFormat, p.Timeout)
			}
			p.Mutex.Unlock()
		}

		timer.Reset(p.getTimeout())
	}

	logrus.Infof("Stopping pod watcher in namespace %s", p.Namespace)
	return err
}

// Stop implements Watcher.
func (p *PodWatcher) Stop() error {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	logrus.Infof("Stopping pod watcher in namespace %s", p.Namespace)

	p.Running = false
	return nil
}

func (p *PodWatcher) isEnabled() bool {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	return p.Enabled
}

func (p *PodWatcher) setRunning(v bool) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	p.Running = v
}

func (p *PodWatcher) getTimeout() time.Duration {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	return p.Timeout
}

func (p *PodWatcher) addPodToList(pod *apicorev1.Pod) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	logrus.Debugf("Current pod list size: %d", len(p.PodList))
	p.PodList = append(p.PodList, pod)
	logrus.Debugf("Final pod list size: %d", len(p.PodList))
}

func (p *PodWatcher) removePodFromList(pod *apicorev1.Pod) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	logrus.Debugf("Current pod list size: %d", len(p.PodList))
	p.PodList = slices.DeleteFunc(p.PodList, func(v *apicorev1.Pod) bool {
		logrus.Debugf("Checking pod %s against %s: %+v", v.Name, pod.Name, v.Name == pod.Name)
		return v.Name == pod.Name
	})
	logrus.Debugf("Final pod list size: %d", len(p.PodList))
}

func (p *PodWatcher) getRandomPod() (*apicorev1.Pod, error) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if len(p.PodList) == 0 {
		return nil, errors.New("No pods found")
	}

	return p.PodList[rand.Intn(len(p.PodList))], nil
}
