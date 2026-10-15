# WoL Manager

WoL Manager `v1.0.0-beta.1` est un gestionnaire Wake-on-LAN Linux centralisé pour plusieurs réseaux, VLAN, groupes, relais et UPS NUT. Il fournit une CLI, un menu, une API REST authentifiée et une orchestration persistante sans Docker ni accès Internet à l'exécution.

> Beta : testez d'abord avec `--dry-run`. Un paquet WoL émis ne prouve ni sa réception ni le réveil d'une machine.

## Fonctions

- configuration YAML déclarative et strictement validée ;
- broadcast local, unicast, broadcast dirigé explicitement autorisé et relais mTLS ;
- dépendances, groupes, politiques UPS et reprise après revalidation ;
- état ICMP/TCP/ARP avec quatre états distincts ;
- découverte passive et diagnostic actif avec liste blanche, budget, débit, délai et audit ;
- API `/api/v1` avec jetons hachés, permissions et limitation de débit ;
- client NUT facultatif ;
- binaire unique, service systemd facultatif.

## Démarrage rapide

```sh
sudo ./install.sh
```

L'installateur sélectionne automatiquement le binaire Linux AMD64/ARM64 signé présent dans `dist/`, contrôle les dépendances et propose le mode systemd. Il n'installe, ne télécharge et n'utilise jamais Go. Si aucun binaire compatible et vérifiable n'est disponible, l'installation s'arrête proprement.

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
```

Le canal `dev` est temporaire et n'est jamais mémorisé comme canal par défaut.

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
- L'automatisation fournie couvre le retour secteur NUT stable, pas un planificateur généraliste.
- Le diagnostic ne peut confirmer la réception que si un relais ou une vérification de disponibilité l'observe.
- Le mode relais conserve ses identifiants anti-rejeu en mémoire pendant 24 heures ; après redémarrage, une revalidation reste nécessaire côté orchestrateur.
- Le client NUT implémente les variables standard nécessaires à la politique, pas toute la console `upscli`.

Licence MIT.
