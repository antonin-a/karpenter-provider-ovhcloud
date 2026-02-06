# Karpenter OVHcloud pour Managed Kubernetes Service (MKS)

## Introduction

Karpenter OVHcloud est une implémentation du projet Karpenter pour OVHcloud Managed Kubernetes Service (MKS). Il permet l'autoscaling automatique et intelligent des nodes de votre cluster Kubernetes basé sur les besoins réels des pods en attente.

### Différence avec le Cluster Autoscaler OVHcloud

| Critère | Cluster Autoscaler | Karpenter OVHcloud |
|---------|-------------------|-------------------|
| **Granularité** | Node Pool entier | Par pod/workload |
| **Sélection flavor** | Fixe par pool | Dynamique selon besoins |
| **Vitesse** | 2-5 minutes | 1-3 minutes |
| **Consolidation** | Non | Oui (regroupement automatique) |
| **Configuration** | Par Node Pool | Centralisée (NodePool CRD) |
| **Multi-zone** | Manuel | Automatique |

---

## Prérequis

- Cluster OVHcloud MKS actif
- Credentials API OVHcloud (Application Key, Secret, Consumer Key)
- `kubectl` et `helm` installés
- Droits administrateur sur le cluster

### Création des credentials OVHcloud

1. Rendez-vous sur [https://api.ovh.com/createToken/](https://api.ovh.com/createToken/)
2. Configurez les droits suivants :
   ```
   GET    /cloud/project/*/kube/*
   POST   /cloud/project/*/kube/*/nodepool
   PUT    /cloud/project/*/kube/*/nodepool/*
   DELETE /cloud/project/*/kube/*/nodepool/*
   GET    /cloud/project/*/kube/*/nodepool
   GET    /cloud/project/*/kube/*/nodepool/*/nodes
   GET    /cloud/project/*/kube/*/flavors
   ```

---

## Installation

### 1. Installer les CRDs

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/main/pkg/apis/crds/karpenter.sh_nodepools.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/main/pkg/apis/crds/karpenter.sh_nodeclaims.yaml
kubectl apply -f ovhcloud/charts/karpenter-ovhcloud/crds/ovhnodeclass-crd.yaml
```

### 2. Créer le Secret avec les credentials

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: ovh-credentials
  namespace: karpenter
type: Opaque
stringData:
  endpoint: "ovh-eu"
  applicationKey: "VOTRE_APPLICATION_KEY"
  applicationSecret: "VOTRE_APPLICATION_SECRET"
  consumerKey: "VOTRE_CONSUMER_KEY"
```

### 3. Installer via Helm

```bash
helm install karpenter-ovhcloud ./ovhcloud/charts/karpenter-ovhcloud \
  --namespace karpenter \
  --create-namespace \
  --set ovh.serviceName="VOTRE_PROJECT_ID" \
  --set ovh.kubeId="VOTRE_CLUSTER_ID" \
  --set ovh.region="GRA7"
```

---

## Configuration

### OVHNodeClass

L'`OVHNodeClass` définit la configuration spécifique à OVHcloud pour les nodes créés par Karpenter.

```yaml
apiVersion: karpenter.ovhcloud.sh/v1alpha1
kind: OVHNodeClass
metadata:
  name: default
spec:
  # Identifiant du projet Public Cloud OVHcloud (obligatoire)
  serviceName: "abc123def456..."

  # Identifiant du cluster MKS (obligatoire)
  kubeId: "cluster-xyz..."

  # Région OVHcloud (obligatoire)
  # Options: GRA7, GRA9, GRA11, SBG5, RBX-A, UK1, DE1, WAW1, BHS5, etc.
  region: "GRA7"

  # Référence au Secret contenant les credentials (optionnel si défini globalement)
  credentialsSecretRef:
    name: ovh-credentials
    namespace: karpenter

  # Facturation mensuelle (optionnel, défaut: false)
  # ⚠️ Disponible uniquement sur instances gen2 : b2, c2, d2, r2
  monthlyBilled: false

  # Anti-affinité entre nodes du même pool (optionnel, défaut: false)
  antiAffinity: false
```

### NodePool

Le `NodePool` définit les règles d'autoscaling et les contraintes sur les nodes.

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: general-purpose
spec:
  template:
    metadata:
      labels:
        # Labels personnalisés appliqués aux nodes
        team: platform
        environment: production
    spec:
      # Référence à l'OVHNodeClass
      nodeClassRef:
        group: karpenter.ovhcloud.sh
        kind: OVHNodeClass
        name: default

      # Exigences sur les nodes
      requirements:
        # Types d'instances autorisés
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["b2-7", "b2-15", "b2-30", "b2-60", "b2-120"]

        # Architecture CPU
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]

        # Type de capacité (on-demand uniquement sur OVHcloud)
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["on-demand"]

        # Zones spécifiques (optionnel)
        - key: topology.kubernetes.io/zone
          operator: In
          values: ["gra7-a", "gra7-b", "gra7-c"]

      # Taints appliqués aux nodes (optionnel)
      taints:
        - key: dedicated
          value: gpu
          effect: NoSchedule

      # Durée avant expiration du node (optionnel)
      # Après ce délai, le node sera remplacé (pour mises à jour, etc.)
      expireAfter: 720h  # 30 jours

  # Limites globales du NodePool (IMPORTANT: toujours définir des limites!)
  limits:
    # Limite CPU totale (recommandé: commencer petit)
    cpu: 100
    # Limite mémoire totale
    memory: 200Gi

  # Politique de disruption (consolidation, etc.)
  disruption:
    # Politique de consolidation
    consolidationPolicy: WhenEmptyOrUnderutilized

    # Délai avant consolidation après scale-down
    consolidateAfter: 30s

    # Budget de disruption (combien de nodes peuvent être perturbés simultanément)
    budgets:
      - nodes: "10%"
      - nodes: "0"
        schedule: "0 9 * * 1-5"  # Pas de disruption pendant les heures de bureau
        duration: 8h
```

---

## Paramètres d'optimisation

### Performance de scaling

| Paramètre | Emplacement | Description | Valeur recommandée |
|-----------|-------------|-------------|-------------------|
| `consolidateAfter` | NodePool.spec.disruption | Délai avant consolidation | `30s` à `5m` |
| `expireAfter` | NodePool.spec.template.spec | Durée de vie max des nodes | `720h` (30j) |
| `limits.cpu` | NodePool.spec | Limite CPU totale | Selon budget |
| `limits.memory` | NodePool.spec | Limite mémoire totale | Selon budget |

### Sélection des instances

| Paramètre | Emplacement | Description | Exemple |
|-----------|-------------|-------------|---------|
| `instance-type` | requirements | Flavors autorisés | `["b2-7", "b2-15"]` |
| `capacity-type` | requirements | Type (on-demand) | `["on-demand"]` |
| `topology.kubernetes.io/zone` | requirements | Zones autorisées | `["gra7-a"]` |

### Flavors OVHcloud disponibles

#### General Purpose (Génération 2)
| Flavor | vCPUs | RAM | Stockage | Usage recommandé |
|--------|-------|-----|----------|------------------|
| b2-7 | 2 | 7 GiB | 50 GiB | Workloads légers, dev |
| b2-15 | 4 | 15 GiB | 100 GiB | Applications standard |
| b2-30 | 8 | 30 GiB | 200 GiB | Bases de données, CI/CD |
| b2-60 | 16 | 60 GiB | 400 GiB | Workloads intensifs |
| b2-120 | 32 | 120 GiB | 400 GiB | Big data, ML |

#### General Purpose (Génération 3 - Recommandé)
| Flavor | vCPUs | RAM | Stockage | Usage recommandé |
|--------|-------|-----|----------|------------------|
| b3-8 | 2 | 8 GiB | 25 GiB | Dev, tests |
| b3-16 | 4 | 16 GiB | 50 GiB | Applications web |
| b3-32 | 8 | 32 GiB | 50 GiB | Microservices |
| b3-64 | 16 | 64 GiB | 50 GiB | Applications intensives |
| b3-128 | 32 | 128 GiB | 50 GiB | Workloads lourds |

#### Compute Optimized
| Flavor | vCPUs | RAM | Stockage | Usage recommandé |
|--------|-------|-----|----------|------------------|
| c2-7 | 2 | 7 GiB | 50 GiB | Calcul léger |
| c2-15 | 4 | 15 GiB | 100 GiB | CI/CD, builds |
| c2-30 | 8 | 30 GiB | 200 GiB | Traitement batch |
| c2-60 | 16 | 60 GiB | 400 GiB | HPC léger |
| c2-120 | 32 | 120 GiB | 400 GiB | HPC |
| c3-8 | 2 | 8 GiB | 25 GiB | Calcul (Gen3) |
| c3-16 | 4 | 16 GiB | 50 GiB | Calcul (Gen3) |
| c3-32 | 8 | 32 GiB | 50 GiB | Calcul (Gen3) |
| c3-64 | 16 | 64 GiB | 50 GiB | Calcul (Gen3) |
| c3-128 | 32 | 128 GiB | 50 GiB | Calcul (Gen3) |

#### Memory Optimized
| Flavor | vCPUs | RAM | Stockage | Usage recommandé |
|--------|-------|-----|----------|------------------|
| r2-15 | 2 | 15 GiB | 50 GiB | Cache, Redis |
| r2-30 | 2 | 30 GiB | 50 GiB | Elasticsearch |
| r2-60 | 4 | 60 GiB | 100 GiB | Bases de données |
| r2-120 | 8 | 120 GiB | 200 GiB | In-memory DBs |
| r2-240 | 16 | 240 GiB | 400 GiB | SAP, grandes BDD |
| r3-16 | 2 | 16 GiB | 25 GiB | Mémoire (Gen3) |
| r3-32 | 2 | 32 GiB | 25 GiB | Mémoire (Gen3) |
| r3-64 | 4 | 64 GiB | 50 GiB | Mémoire (Gen3) |
| r3-128 | 8 | 128 GiB | 50 GiB | Mémoire (Gen3) |
| r3-256 | 16 | 256 GiB | 50 GiB | Mémoire (Gen3) |

#### GPU Instances
| Flavor | vCPUs | RAM | GPU | Usage recommandé |
|--------|-------|-----|-----|------------------|
| t1-45 | 4 | 45 GiB | 1x V100 | ML inference |
| t1-90 | 8 | 90 GiB | 2x V100 | ML training |
| t1-180 | 16 | 180 GiB | 4x V100 | Deep learning |
| t2-45 | 4 | 45 GiB | 1x V100S | ML inference (Gen2) |
| t2-90 | 8 | 90 GiB | 2x V100S | ML training (Gen2) |
| t2-180 | 16 | 180 GiB | 4x V100S | Deep learning (Gen2) |

---

## Exemples de configurations

### Configuration économique (dev/test)

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: dev-pool
spec:
  template:
    spec:
      nodeClassRef:
        group: karpenter.ovhcloud.sh
        kind: OVHNodeClass
        name: default
      requirements:
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["b2-7", "b2-15"]  # Petites instances uniquement
  limits:
    cpu: 50
    memory: 100Gi
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    consolidateAfter: 10s  # Consolidation rapide pour économiser
```

### Configuration production haute disponibilité

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: production-pool
spec:
  template:
    metadata:
      labels:
        environment: production
    spec:
      nodeClassRef:
        group: karpenter.ovhcloud.sh
        kind: OVHNodeClass
        name: default
      requirements:
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["b2-30", "b2-60", "b2-120"]
        - key: topology.kubernetes.io/zone
          operator: In
          values: ["gra7-a", "gra7-b", "gra7-c"]  # Multi-AZ
      expireAfter: 168h  # 7 jours - renouvellement fréquent
  limits:
    cpu: 100
    memory: 200Gi
  disruption:
    consolidationPolicy: WhenEmpty  # Consolidation conservatrice
    consolidateAfter: 5m
    budgets:
      - nodes: "20%"  # Max 20% de nodes perturbés
      - nodes: "0"
        schedule: "0 8 * * 1-5"  # Pas de disruption 8h-18h en semaine
        duration: 10h
```

### Configuration GPU/ML

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: gpu-pool
spec:
  template:
    spec:
      nodeClassRef:
        group: karpenter.ovhcloud.sh
        kind: OVHNodeClass
        name: default
      requirements:
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["t2-45", "t2-90", "t2-180"]  # Instances GPU
      taints:
        - key: nvidia.com/gpu
          value: "true"
          effect: NoSchedule
  limits:
    cpu: 100
```

---

## Test de l'autoscaling

### Déploiement de test (inflate)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inflate
spec:
  replicas: 0
  selector:
    matchLabels:
      app: inflate
  template:
    metadata:
      labels:
        app: inflate
    spec:
      containers:
        - name: inflate
          image: public.ecr.aws/eks-distro/kubernetes/pause:3.7
          resources:
            requests:
              cpu: "1"
              memory: "1Gi"
```

### Commandes de test

```bash
# 1. Vérifier l'état initial
kubectl get nodes
kubectl get nodepools
kubectl get nodeclaims

# 2. Déclencher le scale-up
kubectl scale deployment/inflate --replicas=10

# 3. Observer la création des nodes
kubectl get nodeclaims -w
kubectl get pods -w

# 4. Vérifier les logs Karpenter
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-ovhcloud -f

# 5. Tester le scale-down
kubectl scale deployment/inflate --replicas=0

# 6. Observer la consolidation (après consolidateAfter)
kubectl get nodes -w
```

---

## Dépannage

### Le NodePool reste "Not Ready"

```bash
# Vérifier le status de l'OVHNodeClass
kubectl describe ovhnodeclass default

# La condition Ready doit être True
# Si False, vérifier les credentials et la configuration
```

### Aucun node n'est créé

```bash
# Vérifier les logs Karpenter
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-ovhcloud

# Vérifier les événements
kubectl get events -n karpenter

# Causes fréquentes :
# - Credentials invalides
# - Quota OVHcloud atteint
# - Flavor non disponible dans la région
```

### Erreur "no instance type has enough resources"

Vérifiez que les requirements du NodePool autorisent des flavors avec suffisamment de ressources pour vos pods.

```yaml
# Le pod demande 4 CPUs mais seul b2-7 (2 CPUs) est autorisé
requirements:
  - key: node.kubernetes.io/instance-type
    operator: In
    values: ["b2-7"]  # ❌ Insuffisant

# Solution : autoriser des flavors plus grands
requirements:
  - key: node.kubernetes.io/instance-type
    operator: In
    values: ["b2-7", "b2-15", "b2-30"]  # ✅
```

### Erreur "availabilityZones is mandatory"

Assurez-vous qu'une zone est spécifiée ou que la zone par défaut est configurée :

```yaml
requirements:
  - key: topology.kubernetes.io/zone
    operator: In
    values: ["gra7-a"]  # Zone explicite
```

---

## Architecture interne

### Flux de création d'un node

```
1. Pod Pending (ressources insuffisantes)
        ↓
2. Karpenter détecte le pod
        ↓
3. Calcul du meilleur flavor selon requirements
        ↓
4. Création NodeClaim
        ↓
5. Appel API OVHcloud : création/scale Node Pool
        ↓
6. OVHcloud provisionne la VM
        ↓
7. Node rejoint le cluster
        ↓
8. Pod schedulé sur le nouveau node
```

### Convention de nommage des Node Pools OVHcloud

Karpenter utilise des pools partagés nommés : `karpenter-{flavor}-{zone}`

Exemple : `karpenter-b2-30-gra7-a`

Cette approche permet de :
- Réutiliser les pools existants
- Respecter la limite de 100 pools par cluster
- Optimiser les coûts

---

## Bonnes pratiques

1. **Définir des limites** : Toujours configurer `limits.cpu` et `limits.memory` pour éviter les coûts incontrôlés

2. **Utiliser la consolidation** : Activer `consolidationPolicy: WhenEmptyOrUnderutilized` pour optimiser les coûts

3. **Multi-zone** : Spécifier plusieurs zones pour la haute disponibilité

4. **Budgets de disruption** : Configurer des fenêtres de maintenance pour éviter les disruptions en production

5. **Monitoring** : Surveiller les métriques Karpenter :
   - `karpenter_nodes_created_total`
   - `karpenter_nodes_terminated_total`
   - `karpenter_pods_state`

---

## Liens utiles

- [Documentation Karpenter officielle](https://karpenter.sh/docs/)
- [API OVHcloud Cloud](https://api.ovh.com/console/#/cloud)
- [OVHcloud MKS Documentation](https://help.ovhcloud.com/csm/fr-public-cloud-kubernetes)
- [Repository GitHub Karpenter](https://github.com/kubernetes-sigs/karpenter)
