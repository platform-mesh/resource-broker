/**
 * Generic Resource Service - GraphQL API Client for Dynamic Resource Discovery
 *
 * This service discovers available resource types via GraphQL introspection
 * and provides CRUD operations for any discovered resource.
 */
import { Injectable, inject } from '@angular/core';
import { LuigiContextService } from '@luigi-project/client-support-angular';
import { from, map, Observable, of, switchMap, catchError, filter, shareReplay, take } from 'rxjs';

export interface DiscoveredResource {
  group: string;        // e.g., "compute.generic.platform-mesh.io"
  version: string;      // e.g., "v1alpha1"
  kind: string;         // e.g., "VirtualMachine"
  plural: string;       // e.g., "VirtualMachines"
  graphqlGroup: string; // e.g., "compute_generic_platform_mesh_io"
  category: string;     // e.g., "Compute" or "PKI"
}

export interface ServiceCategory {
  name: string;
  icon: string;
  resources: DiscoveredResource[];
}

export interface GenericResource {
  metadata: {
    name: string;
    namespace?: string;
    creationTimestamp?: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
  };
  spec: Record<string, any>;
  status?: {
    conditions?: Array<{
      type: string;
      status: string;
      reason?: string;
      message?: string;
      lastTransitionTime?: string;
    }>;
    relatedResources?: Record<string, any>;
  };
}

export interface Namespace {
  metadata: {
    name: string;
  };
}

interface NamespaceListResponse {
  v1: {
    Namespaces: {
      items: Namespace[];
    };
  };
}

interface GraphQLConfig {
  endpoint: string;
  token: string | null;
}

// Static configuration for known resource types
// In the future, this could be replaced with dynamic introspection
const KNOWN_RESOURCES: DiscoveredResource[] = [
  {
    group: 'compute.generic.platform-mesh.io',
    version: 'v1alpha1',
    kind: 'VirtualMachine',
    plural: 'VirtualMachines',
    graphqlGroup: 'compute_generic_platform_mesh_io',
    category: 'Compute',
  },
  {
    group: 'pki.generic.platform-mesh.io',
    version: 'v1alpha1',
    kind: 'Certificate',
    plural: 'Certificates',
    graphqlGroup: 'pki_generic_platform_mesh_io',
    category: 'PKI',
  },
];

const SERVICE_CATEGORIES: ServiceCategory[] = [
  {
    name: 'Compute',
    icon: 'it-host',
    resources: KNOWN_RESOURCES.filter(r => r.category === 'Compute'),
  },
  {
    name: 'PKI',
    icon: 'locked',
    resources: KNOWN_RESOURCES.filter(r => r.category === 'PKI'),
  },
];

@Injectable({ providedIn: 'root' })
export class GenericResourceService {
  private luigiContextService = inject(LuigiContextService);
  private discoveredResources$: Observable<DiscoveredResource[]> | null = null;

  /**
   * Extracts GraphQL endpoint and auth token from the Luigi context.
   */
  private getGraphQLConfig(): Observable<GraphQLConfig> {
    return this.luigiContextService.contextObservable().pipe(
      filter((ctx) => {
        const hasContext = !!ctx?.context && Object.keys(ctx.context).length > 0;
        return hasContext;
      }),
      map((ctx) => {
        const context = ctx.context as any;
        const token = context.token || null;
        let endpoint = context.portalContext?.crdGatewayApiUrl;
        if (!endpoint) {
          console.warn('crdGatewayApiUrl not found in context, falling back to default');
          endpoint = context.portalBaseUrl + '/graphql';
        }

        // For local development, convert absolute URLs to relative for proxy
        // The proxy.conf.mjs will forward /api/* to the portal backend
        if (endpoint && window.location.hostname === 'localhost') {
          try {
            const url = new URL(endpoint);
            // Extract the path starting with /api
            const apiPath = url.pathname;
            if (apiPath.startsWith('/api')) {
              endpoint = apiPath;
              console.log('[GenericResourceService] Using proxied endpoint:', endpoint);
            }
          } catch (e) {
            // If it's already a relative URL, use as-is
          }
        }

        return { endpoint, token };
      })
    );
  }

  private buildHeaders(token: string | null): Record<string, string> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }
    return headers;
  }

  /**
   * Get available service categories with their resources.
   */
  getServiceCategories(): ServiceCategory[] {
    return SERVICE_CATEGORIES;
  }

  /**
   * Get all known/discovered resources.
   */
  getDiscoveredResources(): DiscoveredResource[] {
    return KNOWN_RESOURCES;
  }

  /**
   * Find a resource by its group and kind.
   */
  findResource(group: string, kind: string): DiscoveredResource | undefined {
    return KNOWN_RESOURCES.find(
      r => r.group === group && r.kind.toLowerCase() === kind.toLowerCase()
    );
  }

  /**
   * Discover available resources via GraphQL introspection.
   * Currently returns static configuration, but can be extended for dynamic discovery.
   */
  discoverResources(): Observable<DiscoveredResource[]> {
    if (this.discoveredResources$) {
      return this.discoveredResources$;
    }

    // For now, return static configuration
    // In the future, this could use GraphQL introspection:
    // query { __schema { types { name kind fields { name } } } }
    this.discoveredResources$ = of(KNOWN_RESOURCES).pipe(
      shareReplay(1)
    );

    return this.discoveredResources$;
  }

  /**
   * Build a list query for a resource type.
   */
  private buildListQuery(resource: DiscoveredResource): string {
    // Build spec fields based on resource type
    let specFields = '';
    let statusFields = '';

    if (resource.kind === 'VirtualMachine') {
      specFields = `
        spec {
          arch
          memory
        }
      `;
      statusFields = `
        status {
          conditions {
            type
            status
            reason
            message
            lastTransitionTime
          }
          relatedResources
        }
      `;
    } else if (resource.kind === 'Certificate') {
      specFields = `
        spec {
          fqdn
        }
      `;
      statusFields = `
        status {
          conditions {
            type
            status
            reason
            message
            lastTransitionTime
          }
          relatedResources
        }
      `;
    }

    return `
      query List${resource.plural} {
        ${resource.graphqlGroup} {
          ${resource.version} {
            ${resource.plural} {
              items {
                metadata {
                  name
                  namespace
                  creationTimestamp
                  labels
                  annotations
                }
                ${specFields}
                ${statusFields}
              }
            }
          }
        }
      }
    `;
  }

  /**
   * Build a create mutation for a resource type.
   */
  private buildCreateMutation(resource: DiscoveredResource): string {
    // Each resource type has a specific input type in GraphQL
    if (resource.kind === 'VirtualMachine') {
      return `
        mutation CreateVirtualMachine($namespace: String!, $name: String!, $arch: String, $memory: Int) {
          ${resource.graphqlGroup} {
            ${resource.version} {
              createVirtualMachine(
                namespace: $namespace
                object: {
                  metadata: { name: $name }
                  spec: { arch: $arch, memory: $memory }
                }
              ) {
                metadata {
                  name
                  namespace
                }
              }
            }
          }
        }
      `;
    } else if (resource.kind === 'Certificate') {
      return `
        mutation CreateCertificate($namespace: String!, $name: String!, $fqdn: String!) {
          ${resource.graphqlGroup} {
            ${resource.version} {
              createCertificate(
                namespace: $namespace
                object: {
                  metadata: { name: $name }
                  spec: { fqdn: $fqdn }
                }
              ) {
                metadata {
                  name
                  namespace
                }
              }
            }
          }
        }
      `;
    }

    // Fallback (shouldn't reach here with known resources)
    return `
      mutation Create${resource.kind}($namespace: String!, $name: String!) {
        ${resource.graphqlGroup} {
          ${resource.version} {
            create${resource.kind}(
              namespace: $namespace
              object: {
                metadata: { name: $name }
              }
            ) {
              metadata {
                name
                namespace
              }
            }
          }
        }
      }
    `;
  }

  /**
   * Build an update mutation for a resource type.
   */
  private buildUpdateMutation(resource: DiscoveredResource): string {
    if (resource.kind === 'Certificate') {
      return `
        mutation UpdateCertificate($namespace: String!, $name: String!, $fqdn: String!) {
          ${resource.graphqlGroup} {
            ${resource.version} {
              updateCertificate(
                namespace: $namespace
                name: $name
                object: {
                  metadata: { name: $name }
                  spec: { fqdn: $fqdn }
                }
              ) {
                metadata {
                  name
                  namespace
                }
              }
            }
          }
        }
      `;
    }

    // Fallback for other resource types
    return `
      mutation Update${resource.kind}($namespace: String!, $name: String!) {
        ${resource.graphqlGroup} {
          ${resource.version} {
            update${resource.kind}(
              namespace: $namespace
              name: $name
              object: {
                metadata: { name: $name }
              }
            ) {
              metadata {
                name
                namespace
              }
            }
          }
        }
      }
    `;
  }

  /**
   * Build a delete mutation for a resource type.
   */
  private buildDeleteMutation(resource: DiscoveredResource): string {
    return `
      mutation Delete${resource.kind}($name: String!, $namespace: String!) {
        ${resource.graphqlGroup} {
          ${resource.version} {
            delete${resource.kind}(name: $name, namespace: $namespace)
          }
        }
      }
    `;
  }

  /**
   * List all resources of a given type.
   */
  listResources(resource: DiscoveredResource): Observable<GenericResource[]> {
    const query = this.buildListQuery(resource);

    return this.getGraphQLConfig().pipe(
      take(1),
      switchMap(({ endpoint, token }) =>
        from(
          fetch(endpoint, {
            method: 'POST',
            headers: this.buildHeaders(token),
            body: JSON.stringify({ query }),
          }).then((res) => res.json())
        )
      ),
      map((response: any) => {
        const groupData = response.data?.[resource.graphqlGroup];
        const versionData = groupData?.[resource.version];
        const resourceData = versionData?.[resource.plural];
        return resourceData?.items || [];
      }),
      catchError((error) => {
        console.error(`Error fetching ${resource.plural}:`, error);
        return of([]);
      })
    );
  }

  /**
   * Create a new resource.
   */
  createResource(
    resource: DiscoveredResource,
    name: string,
    namespace: string,
    spec: Record<string, any>
  ): Observable<boolean> {
    const mutation = this.buildCreateMutation(resource);

    // Build variables based on resource type
    let variables: Record<string, any> = { namespace, name };

    if (resource.kind === 'VirtualMachine') {
      variables = {
        namespace,
        name,
        arch: spec['arch'] || 'x86_64',
        memory: spec['memory'] || 512,
      };
    } else if (resource.kind === 'Certificate') {
      variables = {
        namespace,
        name,
        fqdn: spec['fqdn'],
      };
    }

    console.log('[GenericResourceService] createResource called:', { resource: resource.kind, name, namespace, spec, variables });
    console.log('[GenericResourceService] mutation:', mutation);

    const requestBody = JSON.stringify({
      query: mutation,
      variables,
    });
    console.log('[GenericResourceService] Request body:', requestBody);

    return this.getGraphQLConfig().pipe(
      take(1),
      switchMap(({ endpoint, token }) => {
        console.log('[GenericResourceService] Sending to endpoint:', endpoint);
        console.log('[GenericResourceService] Variables being sent:', JSON.stringify(variables));
        return from(
          fetch(endpoint, {
            method: 'POST',
            headers: this.buildHeaders(token),
            body: requestBody,
          }).then((res) => res.json())
        );
      }),
      map((response: any) => {
        console.log('[GenericResourceService] Response:', JSON.stringify(response));
        if (response.errors) {
          console.error('GraphQL errors:', response.errors);
          return false;
        }
        const groupData = response.data?.[resource.graphqlGroup];
        const versionData = groupData?.[resource.version];
        return !!versionData?.[`create${resource.kind}`];
      }),
      catchError((error) => {
        console.error(`Error creating ${resource.kind}:`, error);
        return of(false);
      })
    );
  }

  /**
   * Update an existing resource.
   */
  updateResource(
    resource: DiscoveredResource,
    name: string,
    namespace: string,
    spec: Record<string, any>
  ): Observable<boolean> {
    const mutation = this.buildUpdateMutation(resource);

    // Build variables based on resource type
    let variables: Record<string, any> = { namespace, name };

    if (resource.kind === 'Certificate') {
      variables = {
        namespace,
        name,
        fqdn: spec['fqdn'],
      };
    }

    return this.getGraphQLConfig().pipe(
      take(1),
      switchMap(({ endpoint, token }) =>
        from(
          fetch(endpoint, {
            method: 'POST',
            headers: this.buildHeaders(token),
            body: JSON.stringify({
              query: mutation,
              variables,
            }),
          }).then((res) => res.json())
        )
      ),
      map((response: any) => {
        if (response.errors) {
          console.error('GraphQL errors:', response.errors);
          return false;
        }
        const groupData = response.data?.[resource.graphqlGroup];
        const versionData = groupData?.[resource.version];
        return !!versionData?.[`update${resource.kind}`];
      }),
      catchError((error) => {
        console.error(`Error updating ${resource.kind}:`, error);
        return of(false);
      })
    );
  }

  /**
   * Delete a resource.
   */
  deleteResource(
    resource: DiscoveredResource,
    name: string,
    namespace: string
  ): Observable<boolean> {
    const mutation = this.buildDeleteMutation(resource);

    return this.getGraphQLConfig().pipe(
      take(1),
      switchMap(({ endpoint, token }) =>
        from(
          fetch(endpoint, {
            method: 'POST',
            headers: this.buildHeaders(token),
            body: JSON.stringify({
              query: mutation,
              variables: { name, namespace },
            }),
          }).then((res) => res.json())
        )
      ),
      map((response: any) => {
        if (response.errors) {
          console.error('GraphQL errors:', response.errors);
          return false;
        }
        const groupData = response.data?.[resource.graphqlGroup];
        const versionData = groupData?.[resource.version];
        return !!versionData?.[`delete${resource.kind}`];
      }),
      catchError((error) => {
        console.error(`Error deleting ${resource.kind}:`, error);
        return of(false);
      })
    );
  }

  /**
   * List all Namespaces available to the user.
   */
  listNamespaces(): Observable<Namespace[]> {
    const query = `
      query ListNamespaces {
        v1 {
          Namespaces {
            items {
              metadata {
                name
              }
            }
          }
        }
      }
    `;

    return this.getGraphQLConfig().pipe(
      take(1),
      switchMap(({ endpoint, token }) =>
        from(
          fetch(endpoint, {
            method: 'POST',
            headers: this.buildHeaders(token),
            body: JSON.stringify({ query }),
          }).then((res) => res.json())
        )
      ),
      map((response: { data: NamespaceListResponse }) => {
        return response.data?.v1?.Namespaces?.items || [];
      }),
      catchError((error) => {
        console.error('Error fetching namespaces:', error);
        return of([]);
      })
    );
  }
}
