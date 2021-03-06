package routing

import (
	"net"
	"sort"
	"sync"

	"bytes"
	"github.com/golang/glog"
	"github.com/kopeio/route-controller/pkg/util"
	"k8s.io/kubernetes/pkg/api/v1"
)

type NodePredicate func(node *v1.Node) bool

type NodeMap struct {
	util.Stoppable
	mePredicate NodePredicate

	mutex   sync.Mutex
	ready   bool
	nodes   map[string]*NodeInfo
	version uint64
	me      *NodeInfo
}

func (m *NodeMap) IsVersion(version uint64) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.version == version
}

func (m *NodeMap) IsReady() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.ready
}

func (m *NodeMap) Snapshot() (*NodeInfo, []NodeInfo, uint64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.ready {
		return nil, nil, 0
	}

	nodes := make([]NodeInfo, 0, len(m.nodes))
	for _, node := range m.nodes {
		nodes = append(nodes, *node)
	}
	var me NodeInfo
	if m.me != nil {
		me = *m.me
	}
	return &me, nodes, m.version
}

func (m *NodeMap) MarkReady() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.ready = true
}

func (m *NodeMap) RemoveNode(node *v1.Node) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.nodes, node.Name)

	m.version++
}

func (m *NodeMap) UpdateNode(src *v1.Node) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	changed := false
	name := src.Name

	node := m.nodes[name]
	if node == nil {
		node = &NodeInfo{Name: name}
		m.nodes[name] = node
		changed = true
	}
	if node.update(src) {
		changed = true
	}

	if m.me == nil {
		if m.mePredicate(src) {
			m.me = node
			changed = true
		}
	}

	if changed {
		glog.V(2).Infof("Node %q changed", name)
		m.version++
	}

	return changed
}

func NewNodeMap(mePredicate NodePredicate) *NodeMap {
	m := &NodeMap{
		nodes:       make(map[string]*NodeInfo),
		mePredicate: mePredicate,
	}
	return m
}

// NodeInfo contains the subset of the node information that we care about
type NodeInfo struct {
	Name    string
	Address net.IP
	PodCIDR *net.IPNet
}

func (n *NodeInfo) update(src *v1.Node) bool {
	changed := false

	name := src.Name

	cidr := src.Spec.PodCIDR
	if cidr == "" {
		glog.Infof("Node has no CIDR: %q", name)
		if n.PodCIDR != nil {
			changed = true
			n.PodCIDR = nil
		}
	} else {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil || ipnet == nil {
			glog.Warningf("Error parsing CIDR %q for node %q", cidr, name)
			if n.PodCIDR != nil {
				changed = true
				n.PodCIDR = nil
			}
		} else {
			if n.PodCIDR == nil || !ipnet.IP.Equal(n.PodCIDR.IP) || !bytes.Equal(n.PodCIDR.Mask, ipnet.Mask) {
				n.PodCIDR = ipnet
				changed = true
			}
		}
	}

	var internalIPs []string
	for i := range src.Status.Addresses {
		address := &src.Status.Addresses[i]
		if address.Type == v1.NodeInternalIP {
			internalIPs = append(internalIPs, address.Address)
		}
	}

	if len(internalIPs) == 0 {
		if n.Address != nil {
			n.Address = nil
			changed = true
		}
	} else {
		if len(internalIPs) != 1 {
			glog.Infof("arbitrarily choosing IP for node: %q", name)
			sort.Strings(internalIPs) // At least choose consistently
		}

		internalIP := internalIPs[0]
		a := net.ParseIP(internalIP)
		if a == nil {
			glog.Warningf("Unable to parse node address %q", internalIP)
			if n.Address != nil {
				n.Address = nil
				changed = true
			}
		} else if !n.Address.Equal(a) {
			n.Address = a
			changed = true
		}
	}

	return changed
}
