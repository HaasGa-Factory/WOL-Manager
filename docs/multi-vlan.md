# Réseaux, VLAN et relais

Une route IP ne constitue jamais une autorisation WoL.

- `local_broadcast` exige une interface locale dans le CIDR cible et l'autorisation du réseau.
- `unicast` vise uniquement l'IP enregistrée ; son fonctionnement peut dépendre de la table ARP du routeur.
- `directed_broadcast` utilise le broadcast calculé du CIDR et exige `allow_directed_broadcast: true`.
- `relay` est sélectionné uniquement pour le relais affecté au réseau.

Le mode automatique parcourt l'ordre configuré et marque chaque option `selected`, `candidate`, `disabled`, `not_authorized` ou `unavailable`. Il ne transforme jamais un refus en repli permissif.

Le relais est le même binaire. Il exige mTLS, refuse les serveurs inconnus ou désactivés et n'accepte aucune adresse fournie par le client. Exemple :

```sh
wol --config /etc/wol-manager/wol.yaml relay serve \
  --id relay-vlan20 --listen 0.0.0.0:9443 \
  --cert relay.crt --key relay.key --client-ca manager-ca.crt
```
