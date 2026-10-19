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
wol config check
sudo wol --config /etc/wol-manager/wol.yaml token create --id admin
sudo systemctl enable --now wol-manager.service
systemctl status wol-manager.service
```

Conservez immédiatement le jeton affiché par `token create` : il n'est montré qu'une seule fois.
Le service actif relit atomiquement le magasin à chaque authentification : une création est acceptée
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

Le fichier de jetons est préparé en `0600` pour l'utilisateur système `wol-manager`. Lors d'une installation interactive neuve avec activation immédiate du service, l'installateur crée le premier jeton administrateur avant de démarrer systemd et l'affiche une seule fois dans le terminal.

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
| Adresse IPv4 | Adresse utilisée pour vérifier si la machine est joignable et, selon la configuration réseau, pour l'envoi en unicast. Une réservation DHCP est recommandée. |
| Réseau | Réseau WoL auquel appartient la machine. Ici, `lan` doit contenir `192.168.1.237`. |
| Groupes | Groupes auxquels rattacher la machine. Une réponse vide signifie aucun groupe. Saisir `-` sert à vider une valeur existante lors d'une modification. |
| Dépendances | Autres machines que WoL Manager doit réveiller avant celle-ci. Une réponse vide signifie aucune dépendance. |

Le choix `1 — ICMP` convient si TrueNAS répond au ping : WoL Manager l'utilise avant l'envoi pour éviter un réveil inutile, puis après l'envoi pour confirmer que la machine est devenue joignable. Le délai d'une tentative ICMP est de trois secondes. Si le ping est bloqué, utilisez plutôt `2 — TCP` avec un port réellement ouvert sur TrueNAS. Le contrôle ARP indique seulement la présence d'une entrée dans la table de voisinage et ne prouve pas que la machine est allumée.

Le message `Machine truenas prête.` confirme que les valeurs sont valides dans la session en cours. Il ne signifie pas encore qu'elles sont enregistrées sur disque. Revenez au menu principal, choisissez `5 — Résumé, validation et enregistrement`, contrôlez le résumé, puis confirmez `Enregistrer cette configuration ?`.

Après l'enregistrement, effectuez d'abord ces contrôles :

```sh
wol config check
wol status truenas
wol wake truenas --dry-run
```

Le dernier contrôle est une simulation : aucun paquet Wake-on-LAN n'est envoyé.

NUT et l'API sont facultatifs. `wol setup` peut être relancé : il sauvegarde le YAML, préserve les autres modules et écrit atomiquement seulement après validation. Une configuration existante n'est jamais remplacée par l'exemple. Le YAML reste un format interne ; `wol config edit` est réservé aux administrateurs qui souhaitent une édition avancée.

Les journaux d'installation se trouvent dans `/var/log/wol-manager/install-*.log`. Aucun jeton ni contenu de secret n'y est écrit.
