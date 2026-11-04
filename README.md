# WoL Manager

WoL Manager `v1.2.1` est un gestionnaire Wake-on-LAN Linux centralisé pour plusieurs réseaux, VLAN, groupes, relais et UPS NUT.

Il fournit une CLI, un menu interactif, une API REST authentifiée et une reprise persistante après coupure électrique, sans Docker ni accès Internet à l'exécution.

**[Télécharger la dernière version stable — GitHub Releases (Latest)](https://github.com/HaasGa-Factory/WOL-Manager/releases/latest)**

La release GitHub marquée **Latest** correspond toujours à la dernière version stable officielle publiée sur le canal `main`.

> **Précaution :** testez d'abord les opérations de réveil avec `--dry-run`. Un paquet WoL émis ne prouve ni sa réception ni le réveil effectif d'une machine.

## Fonctions

- configuration YAML déclarative et strictement validée ;
- broadcast local, unicast, broadcast dirigé explicitement autorisé et relais mTLS ;
- dépendances, groupes et reprise NUT séquentielle ou parallèle après un cycle de coupure réellement observé ;
- seuils batterie bas/haut, stabilité secteur, politiques d'échec et reprise prudente après redémarrage ;
- contrôles ICMP/TCP/ARP multiples avec logique ET/OU et quatre états distincts ;
- distinction explicite entre héritage des contrôles et absence de vérification ;
- découverte passive et diagnostic actif avec liste blanche, budget, débit, délai et audit ;
- API `/api/v1` avec jetons hachés, permissions et limitation de débit ;
- client NUT facultatif ;
- binaire unique, service systemd facultatif.

## Installation — Version stable

**Systèmes ciblés :** Debian 12 et Debian 13, architectures AMD64 et ARM64.

L'installation depuis une release officielle ne nécessite **ni Git, ni Go, ni Docker**.

### 1. Télécharger la dernière release stable

Ouvrez la page officielle :

**[GitHub — Dernière release stable](https://github.com/HaasGa-Factory/WOL-Manager/releases/latest)**

Dans la section **Assets**, téléchargez l'une des archives d'installation :

- `wol-manager-vX.Y.Z-installer.tar.gz`
- `wol-manager-vX.Y.Z-installer.zip`

`X.Y.Z` représente le numéro de version de la release.

Ces archives contiennent les fichiers nécessaires à l'installation, dont l'installateur et les binaires de distribution.

### 2. Extraire l'archive

Extrayez l'archive téléchargée sur votre machine Debian.

Ouvrez ensuite un terminal dans le répertoire extrait contenant `install.sh`.

### 3. Lancer l'installation

```bash
chmod +x install.sh
sudo ./install.sh
```

L'installateur :

- sélectionne automatiquement le binaire Linux AMD64 ou ARM64 compatible ;
- vérifie les artefacts de distribution et leur intégrité ;
- contrôle les dépendances nécessaires ;
- propose l'installation et l'activation du service systemd ;
- configure l'autocomplétion Bash.

Les binaires sont fournis dans l'archive : **aucune compilation n'est nécessaire**.

Le manifeste signé couvre également la copie de l'installateur système afin que les corrections d'installation suivent les mises à jour futures.

L'installateur n'installe, ne télécharge et n'utilise jamais Go. Si aucun artefact compatible et vérifiable n'est disponible, l'installation s'arrête proprement.

### 4. Configurer WoL Manager

Après l'installation, lancez :

```bash
sudo wol setup
```

L'assistant guide l'ajout des réseaux, machines, groupes, dépendances, paramètres de découverte réseau et serveurs NUT.

Un utilisateur normal n'a pas à modifier manuellement le fichier YAML.

Saisissez `?` dans n'importe quel champ pour obtenir une explication sans changer la valeur.

Chaque enregistrement est validé, contrôlé pour son aptitude opérationnelle, sauvegardé et écrit atomiquement. Une simulation WoL automatique ne transmet aucun Magic Packet.

### 5. Vérifier l'installation

```bash
wol version
wol --help
wol config check
```

Pour consulter les informations relatives à l'UPS et à l'automatisation :

```bash
wol ups test
wol automation status
```

## Gestion des permissions

L'utilisateur réel ayant lancé `sudo ./install.sh` est ajouté de façon idempotente au groupe `wol-manager` afin de lire la configuration protégée sans `sudo`.

Root n'est jamais ajouté.

Ouvrez ensuite une nouvelle session ou exécutez :

```bash
newgrp wol-manager
```

Les écritures administratives (`wol setup`, `wol config ...`, gestion des jetons et du service) continuent à utiliser `sudo`.

Les fichiers protégés ne sont jamais rendus lisibles par tous les utilisateurs.

## Autocomplétion Bash

L'installateur active automatiquement l'autocomplétion Bash.

Dans un nouveau terminal :

- `wol m<Tab>` complète la commande `menu` ;
- `wol config m<Tab>` complète la commande `machine`.

Pour activer immédiatement l'autocomplétion dans un terminal déjà ouvert :

```bash
source /etc/bash_completion.d/wol-manager
```

Ou, sans écrire de fichier :

```bash
source <(wol completion bash)
```

## Aide CLI et menus interactifs

Toutes les commandes disposent d'une aide accessible sans charger la configuration.

Exemples :

```bash
wol --help
wol wake --help
wol config group --help
wol automation status --help
```

Dans les menus interactifs, saisissez `?` pour afficher l'aide du contexte courant.

## API REST et authentification

WoL Manager fournit une API REST authentifiée sous `/api/v1`.

Si vous activez immédiatement le service lors d'une première installation interactive, l'installateur crée et affiche le premier jeton administrateur.

En mode CLI, ou si vous configurez l'API plus tard, vous pouvez gérer les jetons manuellement.

### Créer un jeton administrateur

```bash
sudo wol token create --id admin --permissions read,wake,admin
```

### Lister les jetons

```bash
sudo wol token list
```

### Révoquer un jeton

```bash
sudo wol token revoke --id admin
```

### Exemple d'utilisation de l'API

Le jeton s'utilise dans l'en-tête HTTP `Authorization: Bearer`.

```bash
TOKEN='<jeton-affiché-une-fois>'
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/api/v1/health
```

La création et la révocation des jetons sont prises en compte par l'API dès la requête suivante, sans redémarrer le service.

La commande `token list` n'affiche jamais les secrets ni leurs hashes.

## Mises à jour

WoL Manager intègre son propre mécanisme de mise à jour.

Le canal stable `main` est utilisé par défaut.

### Vérifier les mises à jour stables

```bash
wol update --check
```

### Installer une mise à jour stable

```bash
sudo wol update
```

### Consulter la version installée

```bash
wol version
```

### Vérifier les mises à jour de développement

```bash
wol update --check --channel dev
```

### Installer une version de développement

```bash
sudo wol update --channel dev
```

Le canal `dev` est temporaire et n'est jamais mémorisé comme canal par défaut.

Lors du retour vers `main`, le canal installé est également vérifié : même si la version et la révision source sont identiques, le binaire signé `main` remplace explicitement le binaire `dev` après confirmation.

Si une version `dev` utilisant le manifeste de format 2 est installée alors que `main` publie encore le format signé 1, `wol update --check` explique cette transition et ne modifie rien.

Utilisez `wol update --check --channel dev` jusqu'à la publication de cette version sur `main`. Aucun retour automatique vers une ancienne version n'est tenté.

## Canaux de publication

| Canal | Description | Utilisation |
|---|---|---|
| `main` | Versions stables officielles | Installation et utilisation courante |
| `dev` | Versions de développement | Tests, validations et évolutions |

### Publication des versions stables

Les versions stables sont distribuées sous forme de releases GitHub.

La release marquée **Latest** constitue la référence officielle pour les nouvelles installations stables.

**[Accéder à la dernière release stable](https://github.com/HaasGa-Factory/WOL-Manager/releases/latest)**

Ce lien pointe automatiquement vers la dernière release stable, sans nécessiter de modifier le README à chaque nouvelle publication.

Les archives d'installation permettent de déployer WoL Manager sans cloner le dépôt GitHub et sans installer les outils de développement.

## Essai sans installation

Pour un essai depuis une copie du dépôt contenant les artefacts de distribution :

```bash
cp configs/wol.example.yaml wol.yaml
./dist/wol-linux-amd64 --config ./wol.yaml config check
./dist/wol-linux-amd64 --config ./wol.yaml --json network discover
./dist/wol-linux-amd64 --config ./wol.yaml --json wake storage-1 --dry-run
```

Sur ARM64, remplacez `wol-linux-amd64` par `wol-linux-arm64`.

Cette procédure est distincte de l'installation recommandée depuis la release stable.

## Documentation

- [Architecture](docs/architecture.md)
- [Installation et mise à jour](docs/installation.md)
- [Mises à jour et rollback](docs/updates.md)
- [Compilation et publication locale](docs/development.md)
- [Configuration](docs/configuration.md)
- [CLI](docs/cli.md)
- [API](docs/api.md) et [OpenAPI](api/openapi.yaml)
- [Réseaux et VLAN](docs/multi-vlan.md)
- [Diagnostics](docs/diagnostics.md)
- [NUT](docs/nut.md)
- [Sécurité](docs/security.md)

## Limites connues et validations restantes

- Linux avec `iproute2` est requis pour les routes et voisins détaillés.
- L'installateur cible Debian 12 et 13 sur AMD64/ARM64. Ses scénarios sont testés avec un système simulé ; la validation finale sur quatre VM réelles reste obligatoire avant d'annoncer officiellement la compatibilité.
- Les binaires de distribution sont compilés localement et versionnés dans Git ; GitHub Actions ne compile ni ne publie le logiciel.
- Le stockage JSON atomique vise un service unique ; il ne convient pas à plusieurs instances partageant les mêmes fichiers.
- La reprise automatique exige un cycle NUT observé, une batterie connue lorsque des seuils sont configurés et un service WoL Manager actif pendant la surveillance ; ce n'est pas un planificateur généraliste.
- Une validation matérielle NUT et une exécution ARM64 réelles restent nécessaires avant de qualifier ces chemins pour la production.
- Le diagnostic ne peut confirmer la réception que si un relais ou une vérification de disponibilité l'observe.
- Le mode relais conserve ses identifiants anti-rejeu en mémoire pendant 24 heures ; après redémarrage, une revalidation reste nécessaire côté orchestrateur.
- Le client NUT implémente les variables standard nécessaires à la politique, pas toute la console `upscli`.

## Licence

Licence MIT.
