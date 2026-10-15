# Installation et désinstallation

## Compatibilité visée

- Debian 12 et Debian 13 ;
- Linux AMD64 et ARM64 ;
- systemd facultatif pour un usage CLI seul.

La logique de détection et les scénarios sans modification de l'hôte sont automatisés. La matrice complète doit encore être validée dans de vraies VM Debian 12/13, AMD64/ARM64 avant d'annoncer ces plateformes comme officiellement vérifiées.

## Installation

Depuis le dossier du projet :

```sh
sudo ./install.sh
```

L'installation affiche la version Debian, l'architecture, l'espace libre, les dépendances et les mises à jour Debian connues du cache APT. Elle n'exécute jamais `apt upgrade`, `apt full-upgrade` ou `apt dist-upgrade`.

L'installateur utilise d'abord la distribution locale signée dans `dist/`. Sinon, il résout la branche GitHub `main` vers un commit de publication immuable, puis télécharge depuis ce même commit le manifeste signé et le seul binaire correspondant à l'architecture. Il vérifie la signature Ed25519, le SHA-256, la version, le canal et la révision source intégrés au binaire.

Il ne télécharge jamais les sources, n'installe jamais Go et ne tente aucune compilation. Un binaire absent, incompatible ou non vérifiable provoque un arrêt propre avant remplacement.

La configuration et les données existantes sont conservées. L'installateur propose ensuite : service activé, service installé mais désactivé, ou CLI seule. Si aucune configuration n'existe, l'activation demandée est différée jusqu'à la fin de `wol setup`.

## Dépendances

L'exécution complète utilise `iproute2` et `iputils-ping`. L'installation utilise `ca-certificates`, `curl`, `openssl`, `coreutils`, `passwd` et `libc-bin`. `systemd` reste facultatif pour un usage CLI. Chaque paquet manquant est expliqué et soumis à confirmation. Aucun paquet de compilation (`golang-go`, `build-essential`, `gcc`, `make`) n'est demandé.

## Première configuration

```sh
sudo wol setup
sudoedit /etc/wol-manager/wol.yaml
sudo -u wol-manager wol --config /etc/wol-manager/wol.yaml config check
sudo wol --config /etc/wol-manager/wol.yaml token create --id admin
```

Le fichier de jetons est préparé en `0600` pour l'utilisateur système `wol-manager`. Lors d'une installation interactive neuve avec activation immédiate du service, l'installateur crée le premier jeton administrateur avant de démarrer systemd et l'affiche une seule fois dans le terminal.

NUT et l'API sont facultatifs. `wol setup` peut être relancé : il sauvegarde le YAML, préserve les autres modules et écrit atomiquement seulement après validation. Une configuration existante n'est jamais remplacée par l'exemple.

Les journaux d'installation se trouvent dans `/var/log/wol-manager/install-*.log`. Aucun jeton ni contenu de secret n'y est écrit.
