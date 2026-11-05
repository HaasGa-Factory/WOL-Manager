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

Le compte systemd utilise des restrictions de fichiers, processus et privilèges. L'accès à des interfaces particulières peut nécessiter une adaptation locale soigneusement documentée.

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
