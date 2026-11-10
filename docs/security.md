# Sécurité

- API distante exclusivement en HTTPS ; relais exclusivement en HTTPS mTLS.
- Jetons API aléatoires de 256 bits, hash SHA-256 stocké, comparaison constante.
- Permissions minimales `read`, `wake`, `admin` et limitation par jeton/adresse.
- Corps HTTP limités, JSON strict, délais réseau et en-têtes défensifs.
- Aucune destination réseau libre dans l'API ou le protocole relais.
- Broadcast dirigé et découverte active refusés par défaut.
- Sous-réseaux publics interdits pour la découverte active.
- Audit sans jetons, mots de passe ni clés.
- Écriture d'audit préalable obligatoire avant réveil, automatisme ou découverte active ; rotation
  hebdomadaire, 12 archives, seuil de 10 MiB et compression via logrotate.
- Cardinalité bornée à 4 096 entrées pour le rate limiter API et la déduplication des requêtes relais ;
  identifiants de requête relais limités à 128 octets et expirés après 24 heures.
- Configuration `root:wol-manager` en `0640`; seuls les membres du groupe peuvent la lire. Les
  secrets restent détenus par le compte de service en `0600` et ne sont pas ouverts au groupe client.
- Magasin de jetons relu après chaque remplacement atomique afin qu'une révocation soit effective à
  la requête suivante sans interrompre les requêtes déjà authentifiées.
- Commandes système limitées à `ip`, `ping` et l'éditeur local, avec arguments construits par le programme.
- Sauvegardes administratives stockées par défaut sous `/var/backups/wol-manager`, hors de l'arbre
  `/var/lib/wol-manager` détenu par le service, avec répertoire `0700` et archives `0600`.
- Archives `.wolbackup` bornées en nombre d'entrées et en taille, limitées à des chemins internes
  connus, avec refus des liens symboliques, traversées de chemin, doublons et cibles système hors périmètre.

Le compte systemd utilise des restrictions de fichiers, processus et privilèges. L'accès à des interfaces particulières peut nécessiter une adaptation locale soigneusement documentée.

## Sécurité des sauvegardes

Le niveau par défaut de `wol backup create` sauvegarde seulement la
configuration validée. Les jetons et l'état persistant ne sont inclus que sur
demande explicite. Les binaires, journaux, caches, clés privées et secrets NUT
externes ne sont jamais collectés.

Le manifeste interne et les SHA-256 détectent une corruption accidentelle,
mais la version 1.3.0 ne chiffre ni ne signe les archives. Une personne capable
de modifier l'archive peut recalculer ses sommes. Vérifiez donc la provenance,
conservez le mode `0600` et utilisez un support chiffré pour toute copie hors
machine. Ne stockez jamais une archive dans Git.

Une restauration crée une sauvegarde de sécurité avant l'écriture, refuse les
cibles symboliques et remplace atomiquement les fichiers. La restauration du
state exige l'arrêt du service et suspend les jobs actifs dans l'état
`revalidation_required` afin qu'aucun réveil ne reprenne automatiquement.

Le serveur relais vérifie aujourd'hui la chaîne mTLS et limite les cibles aux machines/réseaux
affectés à son propre identifiant. Il ne contient pas encore de table applicative liant le sujet du
certificat client à un identifiant d'autorité. Tant qu'une telle politique n'est pas définie, chaque
relais doit utiliser une CA cliente dédiée et non partagée. Ce point reste `needs_validation` : une
convention de nom de certificat ne doit pas être inventée dans le protocole existant.

Signaler les vulnérabilités selon [`SECURITY.md`](../SECURITY.md), jamais dans une issue publique avant coordination.

## Mise en service sécurisée

Avant d'exposer l'API, créez un jeton limité au besoin réel :

```sh
sudo wol token create --help
sudo wol token list
```

Conservez le secret affiché une seule fois dans un gestionnaire de secrets.
Ne le placez pas dans un dépôt, un ticket, un journal ou une commande conservée
dans l'historique du shell. Révoquez les jetons inutiles avec :

```sh
sudo wol token revoke --id IDENTIFIANT
```

Pour un usage local, conservez l'écoute sur `127.0.0.1`. Une écoute distante
exige HTTPS et des certificats correctement administrés. Ne désactivez jamais
la vérification TLS dans un exemple de production.

Accordez uniquement les permissions nécessaires : `read` pour consulter,
`wake` pour demander des réveils et `admin` pour les opérations
d'administration. L'API ne permet pas de fournir librement une MAC, une IP ou
une destination WoL : elle résout les identifiants depuis la configuration du
serveur.
