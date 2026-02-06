# Karpenter OVHcloud Demo

Guide de démonstration pas-à-pas de Karpenter sur OVHcloud Managed Kubernetes Service (MKS).

**Durée totale :** 30-45 minutes

## Prérequis

### Outils requis
- `kubectl` configuré avec accès au cluster MKS
- `helm` v3.x
- `envsubst` (généralement inclus dans `gettext`)

### Credentials OVHcloud
Créez les credentials API sur [OVH API Console](https://eu.api.ovh.com/createToken/):

```bash
# Variables d'environnement requises
export OVH_APPLICATION_KEY="votre-application-key"
export OVH_APPLICATION_SECRET="votre-application-secret"
export OVH_CONSUMER_KEY="votre-consumer-key"
export OVH_SERVICE_NAME="votre-project-id"  # 32 caractères hex
export OVH_KUBE_ID="votre-cluster-id"
export OVH_REGION="EU-WEST-PAR"  # ou GRA7, SBG5, etc.
```

### Permissions API requises
```
GET    /cloud/project/*/kube/*
POST   /cloud/project/*/kube/*/nodepool
PUT    /cloud/project/*/kube/*/nodepool/*
DELETE /cloud/project/*/kube/*/nodepool/*
GET    /cloud/project/*/kube/*/nodepool
GET    /cloud/project/*/kube/*/nodepool/*/nodes
GET    /cloud/project/*/kube/*/flavors
GET    /cloud/project/*/capabilities/kube/*
```

## Structure de la démo

| Fichier | Section | Description |
|---------|---------|-------------|
| `00-verify-prereqs.sh` | Setup | Vérification des prérequis |
| `01-namespace-secret.yaml` | Installation | Namespace et credentials |
| `02-ovhnodeclass.yaml` | Installation | Configuration OVHcloud |
| `03-nodepool-basic.yaml` | Autoscaling | NodePool basique |
| `04-inflate.yaml` | Autoscaling | Deployment de test |
| `05-nodepool-multi-flavor.yaml` | Flavors | Multi-flavor NodePool |
| `06-small-workload.yaml` | Flavors | Workload petit |
| `07-large-workload.yaml` | Flavors | Workload grand |
| `08-nodepool-multizone.yaml` | Multi-zone | NodePool HA |
| `09-zone-spread.yaml` | Multi-zone | Deployment zone-spread |
| `10-ovhnodeclass-monthly.yaml` | Avancé | Monthly billing |
| `11-ovhnodeclass-antiaffinity.yaml` | Avancé | Anti-affinity |
| `99-cleanup.sh` | Cleanup | Nettoyage complet |

## Déroulement de la démo

### 1. Vérification des prérequis (pré-démo)

```bash
chmod +x 00-verify-prereqs.sh
./00-verify-prereqs.sh
```

### 2. Installation (5-7 minutes)

```bash
# Installer les CRDs
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/main/pkg/apis/crds/karpenter.sh_nodepools.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/main/pkg/apis/crds/karpenter.sh_nodeclaims.yaml
kubectl apply -f ../apis/crds/karpenter.ovhcloud.sh_ovhnodeclasses.yaml

# Créer namespace et secret
envsubst < 01-namespace-secret.yaml | kubectl apply -f -

# Installer Karpenter via Helm
helm install karpenter-ovhcloud ../charts/karpenter-ovhcloud \
  --namespace karpenter \
  --set ovh.serviceName="${OVH_SERVICE_NAME}" \
  --set ovh.kubeId="${OVH_KUBE_ID}" \
  --set ovh.region="${OVH_REGION}"

# Vérifier l'installation
kubectl get pods -n karpenter
```

### 3. Autoscaling basique (8-10 minutes)

**Terminal 1 - Watch nodes:**
```bash
watch kubectl get nodes
```

**Terminal 2 - Watch nodeclaims:**
```bash
watch kubectl get nodeclaims
```

**Terminal 3 - Actions:**
```bash
# Appliquer la configuration
envsubst < 02-ovhnodeclass.yaml | kubectl apply -f -
kubectl apply -f 03-nodepool-basic.yaml
kubectl apply -f 04-inflate.yaml

# Déclencher le scale-up
kubectl scale deployment/inflate --replicas=3

# Observer:
# 1. Pods en Pending
# 2. NodeClaim créé
# 3. Pool OVH créé (karpenter-b3-8-eu-west-par-a)
# 4. Node rejoint le cluster (~2-3 min)
# 5. Pods Running

# Scale down
kubectl scale deployment/inflate --replicas=0
# Observer la consolidation après 1 minute
```

### 4. Sélection de flavors (5-7 minutes)

```bash
# NodePool avec plusieurs flavors
kubectl apply -f 05-nodepool-multi-flavor.yaml

# Test 1: Petit workload -> sélectionne b3-8
kubectl apply -f 06-small-workload.yaml
kubectl get nodeclaims -o wide

# Test 2: Grand workload -> sélectionne b3-32
kubectl apply -f 07-large-workload.yaml
kubectl get nodeclaims -o wide
```

### 5. Multi-zone (5 minutes)

```bash
kubectl apply -f 08-nodepool-multizone.yaml
kubectl apply -f 09-zone-spread.yaml

# Observer la distribution
kubectl get nodes -L topology.kubernetes.io/zone
```

### 6. Consolidation (5 minutes)

```bash
# Créer plusieurs nodes
kubectl scale deployment/inflate --replicas=10
# Attendre que tous les nodes soient Ready

# Réduire la charge
kubectl scale deployment/inflate --replicas=2

# Observer la consolidation
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-ovhcloud | grep -i consolidat
```

### 7. Fonctionnalités avancées (7-10 minutes)

```bash
# Monthly billing (~50% économies)
envsubst < 10-ovhnodeclass-monthly.yaml | kubectl apply -f -

# Anti-affinity (spread hyperviseurs, max 5 nodes/pool)
envsubst < 11-ovhnodeclass-antiaffinity.yaml | kubectl apply -f -

# Drift detection
kubectl describe nodeclaim <name>
```

### 8. Monitoring et troubleshooting (5 minutes)

```bash
# Logs Karpenter
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-ovhcloud -f

# Status des ressources
kubectl describe nodepool demo-pool
kubectl describe ovhnodeclass default
kubectl get nodeclaims -o wide

# Events
kubectl get events -n karpenter --sort-by='.lastTimestamp'
```

### 9. Cleanup (3 minutes)

```bash
chmod +x 99-cleanup.sh
./99-cleanup.sh
```

## Points clés à retenir

1. **Stratégie Node Pools partagés** : Convention `karpenter-{flavor}-{zone}` pour respecter la limite de 100 pools/cluster

2. **On-demand uniquement** : OVHcloud MKS ne supporte pas les instances spot

3. **Opérations de pool** :
   - Scale up : Incrément `desiredNodes` ou création nouveau pool
   - Scale down : Décrément ou suppression pool (si 1 seul node)

4. **Format zones** : `{region}-a/b/c` (ex: `eu-west-par-a`)

5. **Régions 3-AZ** : EU-WEST-PAR et EU-SOUTH-MIL uniquement

## Flavors OVHcloud disponibles

| Catégorie | Flavors | Description |
|-----------|---------|-------------|
| b3-* | b3-8, b3-16, b3-32, b3-64, b3-128 | General Purpose |
| c3-* | c3-8, c3-16, c3-32, c3-64, c3-128 | Compute Optimized |
| r3-* | r3-16, r3-32, r3-64, r3-128, r3-256 | Memory Optimized |
| t1/t2-* | t1-45, t2-90 | GPU Tesla |
| a10/a100 | a10-45, a100-180 | GPU AI |

## Troubleshooting

| Problème | Diagnostic | Solution |
|----------|------------|----------|
| NodePool Not Ready | `kubectl describe ovhnodeclass` | Vérifier credentials |
| Pas de node créé | Logs Karpenter | Vérifier requirements flavors |
| Timeout création | Logs + OVH Console | Quotas OVH |
| Node ne join pas | `kubectl get nodeclaims` | Vérifier région/zone |

## Liens utiles

- [Documentation Karpenter](https://karpenter.sh/docs/)
- [API OVHcloud](https://eu.api.ovh.com/console/)
- [OVHcloud MKS](https://www.ovhcloud.com/fr/public-cloud/kubernetes/)
