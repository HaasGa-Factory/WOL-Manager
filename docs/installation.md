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

L'installation affiche la version Debian, l'architecture, l'espace libre, les dépendances et les mises à jour Debian connues du cache APT. Elle n'exécute jamais `apt upgrade`, `apt full-upgrade` ou `apt dist-upgrade`. `curl` est contrôlé même lorsqu'un binaire local est disponible, car il est indispensable aux futures vérifications et mises à jour HTTPS.

L'installateur utilise d'abord la distribution locale signée dans `dist/`. Sinon, il résout la branche GitHub `main` vers un commit de publication immuable, puis télécharge depuis ce même commit le manifeste signé, l'installateur et le seul binaire correspondant à l'architecture. Il vérifie la signature Ed25519, les SHA-256, la version, le canal et la révision source intégrés au binaire. L'installateur système se met ensuite à jour atomiquement avec la copie couverte par le manifeste signé.

Il ne télécharge jamais les sources, n'installe jamais Go et ne tente aucune compilation. Un binaire absent, incompatible ou non vérifiable provoque un arrêt propre avant remplacement.

La configuration et les données existantes sont conservées. L'installateur propose ensuite : service activé, service installé mais désactivé, ou CLI seule. Si aucune configuration n'existe, l'activation demandée est différée jusqu'à la fin de `wol setup`.

### Choix du mode systemd

L'installateur affiche :

```text
Service systemd: 1 activer, 2 installer sans activer, 3 CLI seule [2]:
```

| Choix | Effet | Quand l'utiliser |
| --- | --- | --- |
| `1` — activer | Demande l'installation et l'activation du service. Si aucune configuration n'existe, l'installateur propose ensuite de lancer `wol setup`. Une fois la configuration disponible, il crée le premier jeton administrateur si nécessaire, démarre WoL Manager et active son démarrage automatique. Si la configuration est reportée, le service reste désactivé. | Pour une installation déjà prête à fonctionner en permanence comme API et service d'automatisation. |
| `2` — installer sans activer | Installe le fichier de service, mais ne démarre pas WoL Manager et ne l'active pas au démarrage. | **Choix recommandé en cas de doute** : il permet de configurer et tester WoL Manager avant de lancer le service. C'est aussi le choix utilisé en appuyant simplement sur `Entrée`. |
| `3` — CLI seule | N'installe pas le service systemd. Les commandes `wol` restent utilisables manuellement dans le terminal. | Pour un usage exclusivement manuel, sans API ni automatisation démarrée automatiquement en arrière-plan. |

Après le choix `2`, configurez et vérifiez d'abord WoL Manager, puis activez le service lorsque vous êtes prêt :

```sh
sudo wol setup
sudo -u wol-manager wol --config /etc/wol-manager/wol.yaml config check
sudo wol --config /etc/wol-manager/wol.yaml token create --id admin
sudo systemctl enable --now wol-manager.service
systemctl status wol-manager.service
```

Conservez immédiatement le jeton affiché par `token create` : il n'est montré qu'une seule fois.

`enable --now` effectue deux actions : il démarre le service immédiatement et programme son démarrage automatique aux prochains redémarrages. Pour inspecter les messages du service en cas de problème :

```sh
journalctl -u wol-manager.service -n 100 --no-pager
```

## Dépendances

L'exécution complète utilise `iproute2` et `iputils-ping`. L'installation utilise `ca-certificates`, `curl`, `openssl`, `coreutils`, `passwd`, `libc-bin` et `bash-completion`. Ce dernier permet à la touche `Tab` de compléter les commandes `wol`. `systemd` reste facultatif pour un usage CLI. Chaque paquet manquant est expliqué et soumis à confirmation. Aucun paquet de compilation (`golang-go`, `build-essential`, `gcc`, `make`) n'est demandé.

L'autocomplétion est disponible dans les nouveaux terminaux Bash après l'installation :

```sh
wol m<Tab>          # complète ou propose « menu »
wol config m<Tab>   # complète ou propose « machine »
```

WoL Manager installe son propre fichier dans `/etc/bash_completion.d/wol-manager`. Il ne remplace et ne supprime jamais `/usr/share/bash-completion/completions/wol`, qui appartient au paquet Debian et concerne une autre commande portant le même nom.

Si le terminal était déjà ouvert pendant l'installation, ouvrez-en un nouveau ou utilisez :

```sh
source /etc/bash_completion.d/wol-manager
```

Un contournement temporaire sans écriture système reste disponible :

```sh
source <(wol completion bash)
```

L'installateur utilise un `PATH` système déterministe contenant notamment `/usr/sbin`. Ainsi, `wol update --check` ne signale pas faussement l'absence du paquet `passwd` lorsque le `PATH` d'un utilisateur normal ne contient pas `useradd`.

## Première configuration

```sh
sudo wol setup
sudo -u wol-manager wol --config /etc/wol-manager/wol.yaml config check
sudo wol --config /etc/wol-manager/wol.yaml token create --id admin
```

L'assistant ajoute et modifie les réseaux, machines, groupes, dépendances et serveurs NUT. Il détecte les interfaces IPv4, affiche un résumé, demande confirmation avant l'écriture et propose une simulation WoL qui n'envoie aucun paquet. Pour ouvrir directement une section :

```sh
sudo wol config network
sudo wol config machine
sudo wol config group
sudo wol config nut
```

Le fichier de jetons est préparé en `0600` pour l'utilisateur système `wol-manager`. Lors d'une installation interactive neuve avec activation immédiate du service, l'installateur crée le premier jeton administrateur avant de démarrer systemd et l'affiche une seule fois dans le terminal.

NUT et l'API sont facultatifs. `wol setup` peut être relancé : il sauvegarde le YAML, préserve les autres modules et écrit atomiquement seulement après validation. Une configuration existante n'est jamais remplacée par l'exemple. Le YAML reste un format interne ; `wol config edit` est réservé aux administrateurs qui souhaitent une édition avancée.

Les journaux d'installation se trouvent dans `/var/log/wol-manager/install-*.log`. Aucun jeton ni contenu de secret n'y est écrit.
