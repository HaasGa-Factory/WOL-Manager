# Réseaux, VLAN et relais

Une route IP ne constitue jamais une autorisation WoL.

- `local_broadcast` exige une interface locale dans le CIDR cible et l'autorisation du réseau.
- L'ancien transport `unicast` n'est plus opérationnel. `wol setup` propose une conversion confirmée et sauvegardée des anciennes configurations. L'installateur système `1.2.3` ouvre ce même parcours pendant `sudo wol update`; une installation provenant de `1.2.1` doit d'abord amorcer cet installateur signé selon le guide des [mises à jour](updates.md).
- `directed_broadcast` utilise le broadcast calculé du CIDR et exige `allow_directed_broadcast: true`.
- `relay` est sélectionné uniquement pour le relais affecté au réseau.

Le mode automatique parcourt l'ordre configuré et marque chaque option `selected`, `candidate`, `disabled`, `not_authorized` ou `unavailable`. Il ne transforme jamais un refus en repli permissif.

Le relais est le même binaire. Il exige mTLS, refuse les serveurs inconnus ou désactivés et n'accepte aucune adresse fournie par le client. Exemple :

```sh
wol --config /etc/wol-manager/wol.yaml relay serve \
  --id relay-vlan20 --listen 0.0.0.0:9443 \
  --cert relay.crt --key relay.key --client-ca manager-ca.crt
```

## Choisir le transport adapté

| Situation | Choix | Condition essentielle |
|---|---|---|
| Même réseau local | `local_broadcast` | Interface locale et CIDR correspondants |
| Réseau routé autorisant explicitement le broadcast | `directed_broadcast` | `allow_directed_broadcast: true` et infrastructure adaptée |
| Segment distant doté d'un relais | `relay` | Relais déclaré, mTLS et cible configurée |
| Ancienne configuration `unicast` | migration requise | aucun transport unicast opérationnel |

Une machine joignable par ping à travers un VLAN ne garantit pas que son
Magic Packet puisse traverser le routeur. Vérifiez le plan sans émission avec
`wol network test` ou `wol network test IDENTIFIANT`. Ne contournez pas un
refus en ouvrant un broadcast global.
