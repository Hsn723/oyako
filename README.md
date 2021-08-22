# oyako
An inclusion controller for Contour HTTPProxy resources.

## Motivation
Contour's HTTPProxy comes with a feature called inclusion, where HTTPProxy objects can be included in another, forming a parent/child relationship. This is useful in situations where multiple teams share the same FQDN, but manage their own paths within it. The shared FQDN would reside in a root HTTPProxy that delegates paths to their respective teams via the `.spec.includes` block. Individual teams would manage their own paths, possibly delegating further child paths themselves. This would result in something similar to the example below:

```yaml
# root HTTPProxy
apiVersion: projectcontour.io/v1
kind: HTTPProxy
metadata:
  name: example-root
  namespace: root
spec:
  virtualhost:
    fqdn: example.com
  includes:
  - name: blog
    namespace: blog-team
    conditions:
    - prefix: /blog
  - name: sales
    namespace: sales-team
    conditions:
    - prefix: /sales
---
# child blog HTTPProxy
apiVersion: projectcontour.io/v1
kind: HTTPProxy
metadata:
  name: blog
  namespace: blog-team
spec:
  routes:
  - services:
    - name: blog-service
      port: 80
  - services:
    - name: blog-subservice
      port: 80
    conditions:
    - prefix: /sub # example.com/blog/sub
---
# child sales HTTPProxy
apiVersion: projectcontour.io/v1
kind: HTTPProxy
metadata:
  name: sales
  namespace: sales-team
spec:
  routes:
  - services:
    - name: sales-service
      port: 80
  includes:
  - name: hoge
    namespace: hoge-team
    conditions:
    - prefix: /hoge # further delegate example.com/sales/hoge
```

One downside with this approach is that as paths are added/removed the burden falls upon the parent's team to manage the inclusion settings accordingly. The `oyako` controller enables child HTTPProxy objects to update their parent HTTPProxy object to insert themselves in the inclusion list. In a typical scenario, the team in control of the root HTTPProxy is in only charge of managing the FQDN and certificates, and all other teams are free to add their own services.

## Usage
The behavior of `oyako` is controlled via annotations on HTTPProxy objects.

- `oyako.atelierhsn.com/allow-inclusion: "true"`: permit child HTTPProxy objects to designate this object as their parent
- `oyako.atelierhsn.com/parent`: the namespaced name of the parent HTTPProxy (format: `namespace/name`)
- `oyako.atelierhsn.com/prefix`: the prefix under which the child HTTPProxy will be delegated. If not specified, the prefix is assumed to be the name of the child HTTPProxy

## Limitations
`oyako` only allows for inclusion via path prefixes, and will not assign the same prefix to multiple children.
