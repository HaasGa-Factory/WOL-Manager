# Moteur Wake-on-LAN : broadcast et migration

## Décision appliquée

L'unicast n'est plus un transport opérationnel. Les seuls transports acceptés par la validation sont
`local_broadcast`, `directed_broadcast` lorsqu'il est explicitement autorisé, et `relay`. Aucun appel
UDP unicast ni fallback unicast/broadcast ne subsiste dans le moteur.

Le broadcast local est construit à partir de l'interface Linux déclarée. L'interface doit être
présente, active, compatible broadcast et posséder une IPv4 dans le CIDR configuré. Son préfixe doit
correspondre au CIDR déclaré et fournir une adresse de broadcast utilisable (`/30` ou plus large).
Cette IPv4 est imposée comme source de l'envoi. La destination est le broadcast calculé du sous-réseau
et jamais `255.255.255.255` par défaut. Si ces conditions échouent, aucun paquet n'est envoyé.

## Migration contrôlée

- `wol config check`, le démarrage du service et l'API refusent les anciens champs sans modifier le
  fichier.
- `sudo wol setup` reconnaît `unicast` et `allow_unicast`, annonce l'incompatibilité et prépare la
  suppression uniquement après une première confirmation.
- L'installateur système `1.2.3` ouvre ce même assistant avec le binaire candidat signé lorsque
  l'ancienne configuration bloque la validation. Une installation qui utilise encore l'installateur
  `1.2.1` doit d'abord suivre l'amorçage vérifié décrit dans les
  [mises à jour](updates.md).
- Une machine dont la résolution effective était uniquement `unicast` est listée explicitement. Un
  transport broadcast ou relais valide doit être configuré avant l'enregistrement.
- Le résumé et la confirmation finale restent obligatoires.
- Le fichier original reçoit une sauvegarde datée avant l'écriture atomique. Une annulation ne crée
  aucune modification.

## Statuts de preuve

| Champ | Signification |
|---|---|
| `socket_accepted` | Le noyau a accepté l'appel UDP local. Ce n'est ni une capture ni une preuve physique. Il reste faux pour une requête relayée. |
| `packet_emitted` | `not_emitted` si rien n'a été demandé au réseau ; `unverified` après un succès socket ou relais sans observation réseau indépendante. Aucun état `confirmed` n'est inventé. |
| `wake_verified` | La machine est joignable selon les contrôles ICMP, TCP, ARP et leur logique `any`/`all`. |
| `verified` | Alias historique conservé pour la compatibilité de l'API ; même valeur que `wake_verified`. |

Une machine déjà accessible peut être `wake_verified: true` avec `packet_emitted: not_emitted`. Un
`--dry-run` ne transmet rien. Un échec UDP conserve `socket_accepted: false`,
`packet_emitted: not_emitted` et remonte l'erreur. La réussite des dépendances, groupes et politiques
UPS continue d'être évaluée indépendamment de la transmission.

## Non-régression automatisée

Les tests utilisent un résolveur d'interface et un émetteur injectables. Ils couvrent le broadcast
de sous-réseau autorisé, l'interface/source sélectionnée, l'échec UDP, la simulation, la machine déjà
joignable, la vérification réussie ou échouée, l'absence de contrôle, les configurations unicast
mixtes et unicast seules, la sauvegarde de migration, les dépendances, groupes et règles UPS. La suite
complète a réussi avec Go 1.26.10 dans une unité systemd à réseau privé ; aucun Magic Packet réel n'a
été émis.

La confirmation de `packet_emitted` par capture réelle reste volontairement hors de l'application et
doit être réalisée par l'agent TEST sur un réseau de laboratoire explicitement autorisé.

## Vérifier après migration

```sh
wol config check
wol network test
wol wake IDENTIFIANT --dry-run
```

Chaque machine doit disposer d'un transport réellement autorisé. Une ancienne
référence `unicast` n'est pas un repli : utilisez un broadcast local, un
broadcast dirigé explicitement permis ou un relais mTLS. La validation du plan
n'est pas une preuve de réveil physique.
