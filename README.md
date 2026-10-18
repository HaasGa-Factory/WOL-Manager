# WoL Manager

WoL Manager `v1.2.0` est un gestionnaire Wake-on-LAN Linux centralisé pour plusieurs réseaux, VLAN, groupes, relais et UPS NUT. Il fournit une CLI, un menu, une API REST authentifiée et une reprise persistante après coupure électrique, sans Docker ni accès Internet à l'exécution.

> Beta : testez d'abord avec `--dry-run`. Un paquet WoL émis ne prouve ni sa réception ni le réveil d'une machine.

## Fonctions

- configuration YAML déclarative et strictement validée ;
- broadcast local, unicast, broadcast dirigé explicitement autorisé et relais mTLS ;
- dépendances, groupes et reprise NUT séquentielle ou parallèle après un cycle de coupure réellement observé ;
- seuils batterie bas/haut, stabilité secteur, politiques d'échec et reprise prudente après redémarrage ;
- contrôles ICMP/TCP/ARP multiples avec logique ET/OU et quatre états distincts ;
- découverte passive et diagnostic actif avec liste blanche, budget, débit, délai et audit ;
- API `/api/v1` avec jetons hachés, permissions et limitation de débit ;
- client NUT facultatif ;
- binaire unique, service systemd facultatif.

## Démarrage rapide

```sh
sudo ./install.sh
```

L'installateur active aussi l'autocomplétion Bash : dans un nouveau terminal, `wol m<Tab>` complète la commande `menu` et `wol config m<Tab>` complète `machine`.

Pour l'activer immédiatement dans un terminal déjà ouvert :

```sh
source /etc/bash_completion.d/wol-manager
# ou, sans écrire de fichier :
source <(wol completion bash)
```

L'installateur sélectionne automatiquement le binaire Linux AMD64/ARM64 signé présent dans `dist/`, contrôle les dépendances et propose le mode systemd. Le manifeste signé couvre aussi la copie de l'installateur système afin que les corrections d'installation suivent les mises à jour futures. Il n'installe, ne télécharge et n'utilise jamais Go. Si aucun artefact compatible et vérifiable n'est disponible, l'installation s'arrête proprement.

`sudo wol setup` guide ensuite l'ajout des réseaux, machines, groupes, dépendances et serveurs NUT. Un utilisateur normal n'a pas à modifier le YAML. Chaque enregistrement est validé, sauvegardé et écrit atomiquement ; un test WoL en simulation est proposé.

Si vous activez immédiatement le service lors d'une première installation interactive, l'installateur crée et affiche le premier jeton administrateur. En mode CLI ou si vous configurez l'API plus tard :

```sh
sudo wol token create --id admin --permissions read,wake,admin
```

```sh
wol update --check
sudo wol update
wol update --check --channel dev
sudo wol update --channel dev
wol version
wol ups test
wol automation status
```

Le canal `dev` est temporaire et n'est jamais mémorisé comme canal par défaut.

Si une version `dev` utilisant le manifeste de format 2 est installée alors que `main` publie encore le format signé 1, `wol update --check` explique cette transition et ne modifie rien. Utilisez `wol update --check --channel dev` jusqu'à la publication de cette version sur `main` ; aucun retour automatique vers une ancienne version n'est tenté.

Pour un essai sans installation :

```sh
cp configs/wol.example.yaml wol.yaml
./dist/wol-linux-amd64 --config ./wol.yaml config check
./dist/wol-linux-amd64 --config ./wol.yaml --json network discover
./dist/wol-linux-amd64 --config ./wol.yaml --json wake storage-1 --dry-run
```

Sur ARM64, remplacez `wol-linux-amd64` par `wol-linux-arm64`.

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

## Limites connues de la Beta

- Linux avec `iproute2` est requis pour les routes et voisins détaillés.
- L'installateur cible Debian 12 et 13 sur AMD64/ARM64. Ses scénarios sont testés avec un système simulé ; la validation finale sur quatre VM réelles reste obligatoire avant d'annoncer officiellement la compatibilité.
- Les binaires de distribution sont compilés localement et versionnés dans Git ; GitHub Actions ne compile ni ne publie le logiciel.
- Le stockage JSON atomique vise un service unique ; il ne convient pas à plusieurs instances partageant les mêmes fichiers.
- La reprise automatique exige un cycle NUT observé, une batterie connue lorsque des seuils sont configurés et un service WoL Manager actif pendant la surveillance ; ce n'est pas un planificateur généraliste.
- Une validation matérielle NUT et une exécution ARM64 réelles restent nécessaires avant de qualifier ces chemins pour la production.
- Le diagnostic ne peut confirmer la réception que si un relais ou une vérification de disponibilité l'observe.
- Le mode relais conserve ses identifiants anti-rejeu en mémoire pendant 24 heures ; après redémarrage, une revalidation reste nécessaire côté orchestrateur.
- Le client NUT implémente les variables standard nécessaires à la politique, pas toute la console `upscli`.

Licence MIT.
