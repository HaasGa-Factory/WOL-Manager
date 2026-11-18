# Installation et désinstallation

## Compatibilité visée

- Debian 12 et Debian 13 ;
- Linux AMD64 et ARM64 ;
- systemd facultatif pour un usage CLI seul.

La logique de détection et les scénarios sans modification de l'hôte sont automatisés. La matrice complète doit encore être validée dans de vraies VM Debian 12/13, AMD64/ARM64 avant d'annoncer ces plateformes comme officiellement vérifiées.

## Installation

Avant de modifier le système, consultez les options et exécutez le contrôle
sans installation :

```sh
./install.sh --help
sudo ./install.sh --check
./install.sh --version
```

Options disponibles : `--channel main|dev`,
`--service daemon|api|both|install|cli`, `--configure-services`, `--setup`,
`--yes`, `--non-interactive`, `--allow-downgrade`, `--verbose`, `--check`,
`--version`, `--uninstall`, `-h` et `--help`. L'ancien mode
`--service enable` reste un alias de `both`. `--channel dev`
s'applique uniquement à l'exécution courante. `--allow-downgrade` exige une
autorisation explicite ; aucun retour à une ancienne version n'est implicite.

Depuis le dossier du projet :

```sh
sudo ./install.sh
```

`./install.sh --help` décrit les modes systemd et les canaux. `main` est stable et reste le défaut ;
`dev` est un canal BETA choisi uniquement pour la commande courante. Lorsqu'un dossier téléchargé
contient un manifeste local signé, l'installateur vérifie d'abord sa signature puis détecte son canal
et sa version. Un canal explicitement demandé doit correspondre au manifeste et aucun downgrade
implicite n'est accepté.

L'installation affiche la version Debian, l'architecture, l'espace libre, les dépendances et seulement
le nombre de mises à jour Debian connues du cache APT. `--verbose` affiche leur liste dans le terminal ;
le journal la conserve dans tous les cas. L'installateur n'exécute jamais `apt upgrade`,
`apt full-upgrade` ou `apt dist-upgrade`. `curl` est contrôlé même lorsqu'un binaire local est disponible,
car il est indispensable aux futures vérifications et mises à jour HTTPS.

L'installateur utilise d'abord la distribution locale signée dans `dist/`. Sinon, il résout la branche GitHub `main` vers un commit de publication immuable, puis télécharge depuis ce même commit le manifeste signé, l'installateur et le seul binaire correspondant à l'architecture. Il vérifie la signature Ed25519, les SHA-256, la version, le canal et la révision source intégrés au binaire. L'installateur système se met ensuite à jour atomiquement avec la copie couverte par le manifeste signé.

Il ne télécharge jamais les sources, n'installe jamais Go et ne tente aucune compilation. Un binaire absent, incompatible ou non vérifiable provoque un arrêt propre avant remplacement.

La configuration, les jetons, les jobs et les cycles existants sont conservés.
L'installateur propose séparément le daemon NUT et l'API. Si aucune
configuration n'existe, l'activation demandée est différée jusqu'à la fin de
`wol setup`.

### Choix du mode systemd

L'installateur affiche :

```text
Services systemd: 1 daemon NUT, 2 API, 3 daemon + API,
4 CLI seule, 5 installer sans activer [5]:
```

| Choix | Effet | Quand l'utiliser |
| --- | --- | --- |
| `1` — daemon NUT | Installe et active `wol-manager.service`. Il exécute les jobs, surveille NUT et applique les reprises, sans ouvrir de port HTTP. | Pour l'automatisation sans API. |
| `2` — API | Installe et active uniquement `wol-manager-api.service`. Les consultations restent possibles, mais les jobs sont refusés avec `503 daemon_unavailable` tant que le daemon est absent. | Pour une API de consultation ou une activation du daemon différée. |
| `3` — daemon + API | Installe et active les deux unités indépendantes. Le premier jeton administrateur est créé si nécessaire. | **Choix complet confirmé** : automatisation permanente et API REST. |
| `4` — CLI seule | N'installe aucune unité systemd. | Pour un usage manuel sans processus permanent. |
| `5` — installer sans activer | Installe les deux unités sans les démarrer ni les activer au boot. C'est le défaut avec Entrée. | Pour configurer et tester avant activation. |

#### Choix 1 — Daemon NUT uniquement

Ce choix installe et active `wol-manager.service`. Le daemon fonctionne en
arrière-plan, surveille les UPS NUT configurés, conserve les cycles et les
jobs, puis applique les politiques de reprise après une coupure réellement
observée. Il redémarre automatiquement avec Debian et après une défaillance.

L'API REST n'est ni installée ni activée et aucun port HTTP WoL Manager n'est
ouvert. La CLI, `wol menu`, les diagnostics et les commandes manuelles restent
disponibles.

Utilisez ce choix pour automatiser les reprises NUT sans exposer d'API :

```sh
systemctl status wol-manager.service
sudo wol automation status
journalctl -u wol-manager.service -n 100 --no-pager
```

#### Choix 2 — API uniquement

Ce choix installe et active uniquement `wol-manager-api.service`. L'API REST
est disponible, mais aucun daemon ne surveille NUT et aucune reprise
automatique n'est exécutée. Les opérations API qui doivent créer ou suivre un
job renvoient `503 daemon_unavailable` tant que le daemon est absent.

Ce mode convient surtout à une consultation de l'API, à sa préparation ou à
un déploiement dans lequel le daemon sera ajouté plus tard. Il ne constitue
pas une installation d'automatisation complète.

```sh
systemctl status wol-manager-api.service
journalctl -u wol-manager-api.service -n 100 --no-pager
```

#### Choix 3 — Daemon NUT et API

Ce choix installe et active les deux unités indépendantes :

- `wol-manager.service` possède les jobs, l'état persistant et la surveillance
  NUT ;
- `wol-manager-api.service` expose l'API authentifiée et délègue les actions au
  daemon par un socket Unix local protégé.

Un seul moniteur NUT fonctionne : l'API ne démarre pas un second moteur. Elle
peut être arrêtée sans interrompre les automatismes. Si aucun jeton n'existe,
l'installateur crée le premier jeton administrateur et n'affiche son secret
qu'une seule fois.

Utilisez ce choix lorsque l'automatisation NUT et l'accès API sont tous deux
nécessaires :

```sh
systemctl status wol-manager.service
systemctl status wol-manager-api.service
sudo wol automation status
```

#### Choix 4 — CLI uniquement

Ce choix installe la commande `wol`, mais aucune unité systemd WoL Manager.
Rien ne reste actif en arrière-plan et rien ne démarre automatiquement avec
Debian. Les commandes manuelles restent disponibles, notamment `wol menu`,
`wol status`, `wol ups test` et `wol wake`.

Sans daemon, aucune surveillance NUT permanente et aucune reprise automatique
ne sont possibles. Sans service API, aucun port HTTP WoL Manager n'est ouvert.
Utilisez ce choix pour un poste d'administration manuelle ou des essais sans
service permanent.

#### Choix 5 — Installer les deux unités sans les activer

Ce choix installe les fichiers `wol-manager.service` et
`wol-manager-api.service`, mais les laisse arrêtés et désactivés au démarrage.
Il permet de terminer la configuration, de vérifier les permissions et de
choisir ensuite précisément les composants à démarrer.

Tant qu'aucune unité n'est activée, aucune surveillance NUT permanente, aucune
reprise automatique et aucune API ne fonctionnent. Ce choix est la valeur par
défaut lorsque l'utilisateur appuie simplement sur Entrée.

Après le choix `5`, configurez et vérifiez d'abord WoL Manager, puis activez
les composants lorsque vous êtes prêt :

```sh
sudo wol setup
wol config check
sudo wol --config /etc/wol-manager/wol.yaml token create --id admin
sudo systemctl enable --now wol-manager.service wol-manager-api.service
systemctl status wol-manager.service
systemctl status wol-manager-api.service
```

Conservez immédiatement le jeton affiché par `token create` : il n'est montré qu'une seule fois.
Le service API actif relit atomiquement le magasin à chaque authentification : une création est acceptée
et une révocation est refusée dès la requête API suivante, sans redémarrage.

### Permissions de l'utilisateur

`/etc/wol-manager` reste `root:wol-manager` en `0750` et `wol.yaml` reste
`root:wol-manager` en `0640`. Pendant une installation ou une mise à jour lancée avec `sudo`, le
compte indiqué par `SUDO_USER` est ajouté une seule fois au groupe `wol-manager`; `root` n'est jamais
ajouté comme utilisateur client. Les secrets conservent leurs permissions plus strictes (`0600`).

L'appartenance au groupe n'est visible qu'après une nouvelle connexion. Vous pouvez l'appliquer au
terminal courant avec :

```sh
newgrp wol-manager
```

Après cela, les lectures documentées fonctionnent sans `sudo` :

```sh
wol config check
wol list
wol status truenas
wol wake truenas --dry-run
wol network test truenas
```

Un utilisateur qui n'appartient pas à `wol-manager` ne peut pas lire la configuration. Les commandes
qui écrivent sous `/etc`, gèrent les jetons ou systemd restent administratives et utilisent `sudo`.

`enable --now` démarre les unités immédiatement et programme leur démarrage
automatique aux prochains redémarrages. Pour inspecter séparément le daemon et
l'API :

```sh
journalctl -u wol-manager.service -n 100 --no-pager
journalctl -u wol-manager-api.service -n 100 --no-pager
```

Le daemon peut être arrêté ou redémarré sans arrêter l'API, et l'API peut être
désactivée sans interrompre la surveillance NUT. Une modification du YAML
nécessite de redémarrer les composants qui le lisent.

### Modifier les composants après l'installation

Il n'est pas nécessaire de réinstaller le programme :

```sh
sudo /usr/local/lib/wol-manager/install.sh --configure-services --service daemon
sudo /usr/local/lib/wol-manager/install.sh --configure-services --service api
sudo /usr/local/lib/wol-manager/install.sh --configure-services --service both
sudo /usr/local/lib/wol-manager/install.sh --configure-services --service install
sudo /usr/local/lib/wol-manager/install.sh --configure-services --service cli
```

> **Important :** la valeur de `--service` décrit l'ensemble final des
> composants souhaités ; elle ne s'ajoute pas aux services déjà installés.
> Ainsi, `--service api` bascule vers une installation **API uniquement** et
> retire l'unité du daemon. Pour ajouter l'API à un daemon NUT déjà installé
> tout en conservant l'automatisation, utilisez obligatoirement
> `--service both`.

#### Ajouter l'API après une installation « daemon NUT uniquement »

Si le choix `1` a été utilisé lors de l'installation initiale, ajoutez plus
tard l'API avec :

```sh
sudo /usr/local/lib/wol-manager/install.sh \
  --configure-services \
  --service both
```

Cette opération conserve la configuration, les secrets, les jobs, les cycles
et la surveillance NUT. Elle installe et active l'unité API en plus du daemon.
Si aucun jeton API n'existe, le premier jeton administrateur est créé et son
secret n'est affiché qu'une seule fois : conservez-le immédiatement dans un
emplacement sûr.

Vérifiez ensuite les deux composants :

```sh
systemctl status wol-manager.service
systemctl status wol-manager-api.service
sudo wol automation status
```

Un arrêt ultérieur de `wol-manager-api.service` n'interrompt pas le daemon ni
la reprise automatique NUT.

`install` conserve les deux fichiers d'unité sans les activer ; `cli` arrête,
désactive et retire les deux unités sans supprimer la configuration, les
secrets ni l'état.

### Migration depuis l'ancien service combiné

Les versions antérieures lançaient l'API et la surveillance NUT dans
`wol-manager.service`. Lors du premier saut depuis v1.2.3, l'ancien installateur
remplace le binaire et sa propre copie, mais ne sait pas encore créer les deux
unités séparées. Le nouveau binaire conserve temporairement la surveillance et
l'API avec un socket privé inscriptible, sans dépendre de `/run/wol-manager`.

Relancez la même commande de mise à jour : le nouvel installateur migre alors
automatiquement les unités, même si la version du binaire est déjà à jour.

Canal de développement :

```sh
sudo wol update --channel dev
systemctl status wol-manager.service wol-manager-api.service
```

Canal stable :

```sh
sudo wol update
systemctl status wol-manager.service wol-manager-api.service
```

La migration sauvegarde les unités précédentes et le binaire, préserve le YAML,
les jetons, les jobs et les cycles, puis installe les deux services séparés.
Elle conserve les états actif/inactif et activé/désactivé, ne modifie et ne
redémarre jamais `nut-server` ou `nut-monitor`, et restaure l'ancienne
installation si une vérification échoue.

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

Lorsqu'une mise à jour exécutée par l'installateur système `1.2.3` détecte une ancienne configuration
`unicast`, elle ne la modifie jamais silencieusement. Le binaire signé candidat ouvre l'assistant de
migration avec résumé, sauvegarde et confirmation. Une installation encore équipée de
l'installateur `1.2.1` doit d'abord suivre la procédure d'amorçage du guide des
[mises à jour](updates.md).

Après l'installation d'une version `dev`, `wol update --check` continue de consulter `main` par défaut. Si `main` utilise encore l'ancien manifeste signé de format 1, la commande indique qu'il faut utiliser temporairement `wol update --check --channel dev` ou attendre la publication sur `main`. Elle ne modifie aucun fichier et ne tente aucun retour vers une ancienne version.

## Première configuration

```sh
sudo wol setup
wol config check
sudo wol --config /etc/wol-manager/wol.yaml token create --id admin
```

L'assistant ajoute et modifie les réseaux, machines, groupes, dépendances et serveurs NUT. Il détecte les interfaces IPv4, affiche un résumé, demande confirmation avant l'écriture et propose une simulation WoL qui n'envoie aucun paquet. Pour ouvrir directement une section :

```sh
sudo wol config network
sudo wol config machine
sudo wol config group
sudo wol config nut
```

Le fichier de jetons est préparé en `0600` pour l'utilisateur système
`wol-manager`. Lors d'une installation interactive neuve avec activation
immédiate de l'API, l'installateur crée le premier jeton administrateur avant
de démarrer systemd et l'affiche une seule fois dans le terminal. Le daemon
seul n'exige aucun jeton.

## Première sauvegarde

Après validation de la configuration, créez une sauvegarde de référence :

```sh
sudo wol backup create
sudo wol backup list
```

La configuration seule est le niveau recommandé par défaut. Les archives
système sont placées sous `/var/backups/wol-manager`, séparées de l'état détenu
par le service. Pour conserver aussi les hashes de jetons ou les jobs/cycles,
utilisez respectivement `--with-tokens` ou `--full`.

Avant toute restauration, utilisez `--dry-run`. Consultez le guide
[Sauvegarde et restauration](backups.md) pour les restaurations sélectives, la
sauvegarde de sécurité, l'arrêt du service et les limites de confidentialité.

### Ajouter une machine

Exemple pour un serveur TrueNAS :

```text
Identifiant de la machine: truenas
Nom: TrueNAS
Adresse MAC: 9c:6b:00:84:27:a0
Adresse IPv4: 192.168.1.237
Réseau: lan
Groupes: laisser vide si aucun groupe n'est nécessaire
Dépendances machines: laisser vide si aucune autre machine ne doit être réveillée avant TrueNAS
Vérification après réveil: 1 — ICMP
```

| Champ | Rôle |
| --- | --- |
| Identifiant | Nom technique stable utilisé dans les commandes, par exemple `wol wake truenas`. |
| Nom | Libellé lisible affiché à l'utilisateur. |
| Adresse MAC | Adresse de la carte réseau qui recevra le paquet Wake-on-LAN. |
| Adresse IPv4 | Adresse utilisée pour vérifier si la machine est joignable. Une réservation DHCP est recommandée. |
| Réseau | Réseau WoL auquel appartient la machine. Ici, `lan` doit contenir `192.168.1.237`. |
| Groupes | Groupes auxquels rattacher la machine. Une réponse vide signifie aucun groupe. Saisir `-` sert à vider une valeur existante lors d'une modification. |
| Dépendances | Autres machines que WoL Manager doit réveiller avant celle-ci. Une réponse vide signifie aucune dépendance. |

Le choix `1 — ICMP` convient si TrueNAS répond au ping : WoL Manager l'utilise avant l'envoi pour éviter un réveil inutile, puis après l'envoi pour confirmer que la machine est devenue joignable. Le délai d'une tentative ICMP est de trois secondes. Si le ping est bloqué, utilisez plutôt `2 — TCP` avec un port réellement ouvert sur TrueNAS. Le contrôle ARP indique seulement la présence d'une entrée dans la table de voisinage et ne prouve pas que la machine est allumée. Le choix `4 — Aucune vérification` désactive explicitement tout contrôle pour cette machine : son état reste `unknown` et aucun délai de vérification n'est appliqué. Le choix `6 — Hériter des vérifications globales` conserve au contraire les contrôles définis au niveau supérieur.

Le message `Machine truenas prête.` confirme que les valeurs sont valides dans la session en cours. Il ne signifie pas encore qu'elles sont enregistrées sur disque. Revenez au menu principal, choisissez `5 — Résumé, validation et enregistrement`, contrôlez le résumé, puis confirmez `Enregistrer cette configuration ?`.

Après l'enregistrement, effectuez d'abord ces contrôles :

```sh
wol config check
wol status truenas
wol wake truenas --dry-run
```

Le dernier contrôle est une simulation : aucun paquet Wake-on-LAN n'est envoyé. La simulation proposée
par l'assistant affiche les machines sous forme de liste numérotée et accepte soit le numéro, soit
l'identifiant technique.

NUT et l'API sont facultatifs. `wol setup` peut être relancé : il sauvegarde le YAML, préserve les autres modules et écrit atomiquement seulement après validation. Une configuration existante n'est jamais remplacée par l'exemple. Le YAML reste un format interne ; `wol config edit` est réservé aux administrateurs qui souhaitent une édition avancée.

Les journaux d'installation se trouvent dans
`/var/log/wol-manager-installer/install-*.log`. Aucun jeton ni contenu de
secret n'y est écrit. L'installateur déploie aussi
`/etc/logrotate.d/wol-manager` pour le journal d'audit JSONL : rotation
hebdomadaire, 12 archives, seuil de 10 MiB et compression. Une mise à jour
sauvegarde et restaure ce fichier lors d'un rollback ; une désinstallation le
retire sans supprimer les journaux existants.

Le répertoire `/var/log/wol-manager` appartient à `wol-manager:wol-manager`
avec le mode `0750`. L'installateur crée ou normalise
`/var/log/wol-manager/audit.jsonl` en `wol-manager:wol-manager` avec le mode
`0640`, sans vider son contenu. La règle logrotate recrée le fichier avec les
mêmes propriétaire, groupe et permissions après chaque rotation.
