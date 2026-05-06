# microservices-demo — segspec demo fixture

Trimmed adaptation of Google Cloud Platform's
[microservices-demo](https://github.com/GoogleCloudPlatform/microservices-demo)
(Apache 2.0). Reduced from 11 services to 6 (frontend, cartservice,
checkoutservice, productcatalogservice, recommendationservice, redis-cart)
so the corpus stays small while still demonstrating a realistic gRPC
service-mesh dependency graph.

The dependency edges (frontend -> all backend services, checkout ->
shipping/payment/email, cart -> redis) reflect the real Online Boutique
architecture and are exactly the kind of graph operators want pre-baked
NetworkPolicies for.

License: Apache 2.0. See `LICENSE-NOTICE` for the upstream copyright.
