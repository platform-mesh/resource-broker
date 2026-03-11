/**
 * Generic Resource Service - Dynamic GraphQL API Client
 *
 * Discovers available resource types via GraphQL introspection
 * and provides CRUD operations for any discovered resource.
 */
import { Injectable, inject } from '@angular/core';
import { LuigiContextService } from '@luigi-project/client-support-angular';
import { from, map, Observable, of, switchMap, catchError, filter, shareReplay, take } from 'rxjs';

export interface SpecField {
  name: string;
  graphqlType: string;
  scalarType: 'string' | 'number' | 'boolean';
  required: boolean;
}

export interface DiscoveredResource {
  group: string;
  version: string;
  kind: string;
  plural: string;
  graphqlGroup: string;
  category: string;
  specFields: SpecField[];
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

// Category display configuration - maps group prefix to icon and sort order.
// Unknown categories get a fallback icon and are sorted last.
const CATEGORY_CONFIG: Record<string, { icon: string; order: number }> = {
  compute: { icon: 'it-host', order: 1 },
  identity: { icon: 'locked', order: 2 },
  networking: { icon: 'connected', order: 3 },
  storage: { icon: 'cloud', order: 4 },
  databases: { icon: 'database', order: 5 },
  messaging: { icon: 'discussion', order: 6 },
  applications: { icon: 'grid', order: 7 },
  ai: { icon: 'lightbulb', order: 8 },
};

// Display name overrides for categories that need special casing
const CATEGORY_DISPLAY_NAMES: Record<string, string> = {
  ai: 'AI',
};

export const CATEGORY_ICONS: Record<string, string> = Object.fromEntries(
  Object.entries(CATEGORY_CONFIG).map(([k, v]) => [
    CATEGORY_DISPLAY_NAMES[k] || k.charAt(0).toUpperCase() + k.slice(1),
    v.icon,
  ])
);

// Introspection query - fetches all types with their fields and input types.
const INTROSPECTION_QUERY = `{
  __schema {
    queryType { name }
    mutationType { name }
    types {
      name
      kind
      fields {
        name
        type {
          name
          kind
          ofType { name kind ofType { name kind ofType { name kind } } }
        }
        args {
          name
          type {
            name
            kind
            ofType { name kind ofType { name kind ofType { name kind } } }
          }
        }
      }
      inputFields {
        name
        type {
          name
          kind
          ofType { name kind ofType { name kind ofType { name kind } } }
        }
      }
    }
  }
}`;

function resolveTypeName(type: any): string {
  if (type?.name) return type.name;
  if (type?.ofType) return resolveTypeName(type.ofType);
  return '';
}

function resolveScalarType(type: any): 'string' | 'number' | 'boolean' {
  const name = resolveTypeName(type);
  if (name === 'Int' || name === 'Float') return 'number';
  if (name === 'Boolean') return 'boolean';
  return 'string';
}

function resolveGraphQLVarType(type: any): { graphqlType: string; required: boolean } {
  if (type?.kind === 'NON_NULL') {
    return { graphqlType: resolveTypeName(type.ofType) + '!', required: true };
  }
  return { graphqlType: resolveTypeName(type), required: false };
}

function deriveCategoryFromGroup(graphqlGroup: string): string {
  // e.g., "compute_generic_platform_mesh_io" → "Compute"
  const prefix = graphqlGroup.split('_generic_platform_mesh_io')[0];
  return CATEGORY_DISPLAY_NAMES[prefix] || prefix.charAt(0).toUpperCase() + prefix.slice(1);
}

function deriveGroupFromGraphQL(graphqlGroup: string): string {
  // e.g., "compute_generic_platform_mesh_io" → "compute.generic.platform-mesh.io"
  const prefix = graphqlGroup.split('_generic_platform_mesh_io')[0];
  return `${prefix}.generic.platform-mesh.io`;
}

@Injectable({ providedIn: 'root' })
export class GenericResourceService {
  private luigiContextService = inject(LuigiContextService);
  private discoveredResources$: Observable<DiscoveredResource[]> | null = null;

  private getGraphQLConfig(): Observable<GraphQLConfig> {
    return this.luigiContextService.contextObservable().pipe(
      filter((ctx) => {
        return !!ctx?.context && Object.keys(ctx.context).length > 0;
      }),
      map((ctx) => {
        const context = ctx.context as any;
        const token = context.token || null;
        let endpoint = context.portalContext?.crdGatewayApiUrl;
        if (!endpoint) {
          console.warn('crdGatewayApiUrl not found in context, falling back to default');
          endpoint = context.portalBaseUrl + '/graphql';
        }
        if (endpoint && window.location.hostname === 'localhost') {
          try {
            const url = new URL(endpoint);
            const apiPath = url.pathname;
            if (apiPath.startsWith('/api')) {
              endpoint = apiPath;
            }
          } catch (e) {
            // relative URL, use as-is
          }
        }
        return { endpoint, token };
      })
    );
  }

  private buildHeaders(token: string | null): Record<string, string> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }
    return headers;
  }

  /**
   * Discover resources via GraphQL schema introspection.
   * Finds all *_generic_platform_mesh_io groups and their resource types.
   */
  discoverResources(): Observable<DiscoveredResource[]> {
    if (this.discoveredResources$) return this.discoveredResources$;

    this.discoveredResources$ = this.getGraphQLConfig().pipe(
      take(1),
      switchMap(({ endpoint, token }) =>
        from(this.introspectSchema(endpoint, token))
      ),
      catchError((error) => {
        console.error('Failed to introspect schema:', error);
        return of([]);
      }),
      shareReplay(1)
    );

    return this.discoveredResources$;
  }

  private async introspectSchema(
    endpoint: string,
    token: string | null
  ): Promise<DiscoveredResource[]> {
    const response = await fetch(endpoint, {
      method: 'POST',
      headers: this.buildHeaders(token),
      body: JSON.stringify({ query: INTROSPECTION_QUERY }),
    }).then((res) => res.json());

    const schema = response.data?.__schema;
    if (!schema) {
      console.error('[GenericResourceService] No schema in introspection response:', response);
      return [];
    }

    // Build type map for local traversal
    const typeMap = new Map<string, any>();
    for (const type of schema.types) {
      typeMap.set(type.name, type);
    }

    const queryType = typeMap.get(schema.queryType.name);
    if (!queryType?.fields) {
      console.error('[GenericResourceService] Query type has no fields');
      return [];
    }

    // Also get mutation root type (mutations may be on a separate type)
    const mutationType = schema.mutationType
      ? typeMap.get(schema.mutationType.name)
      : null;

    // Build a map of group → version → mutation version type (for extracting kind names)
    const mutationVersionTypes = new Map<string, any>();
    if (mutationType?.fields) {
      for (const mGroupField of mutationType.fields) {
        if (!mGroupField.name.endsWith('_generic_platform_mesh_io')) continue;
        const mGroupTypeName = resolveTypeName(mGroupField.type);
        const mGroupType = typeMap.get(mGroupTypeName);
        if (!mGroupType?.fields) continue;
        for (const mVersionField of mGroupType.fields) {
          if (mVersionField.name.startsWith('__')) continue;
          const mVersionTypeName = resolveTypeName(mVersionField.type);
          const mVersionType = typeMap.get(mVersionTypeName);
          if (mVersionType) {
            mutationVersionTypes.set(`${mGroupField.name}/${mVersionField.name}`, mVersionType);
          }
        }
      }
    }

    const resources: DiscoveredResource[] = [];

    console.log('[GenericResourceService] All query root fields:',
      queryType.fields.map((f: any) => f.name));
    console.log('[GenericResourceService] Mutation root type:',
      schema.mutationType?.name || 'none');

    // Find all generic platform-mesh groups from query root
    for (const groupField of queryType.fields) {
      if (!groupField.name.endsWith('_generic_platform_mesh_io')) continue;

      const graphqlGroup = groupField.name;
      const group = deriveGroupFromGraphQL(graphqlGroup);
      const category = deriveCategoryFromGroup(graphqlGroup);
      const groupTypeName = resolveTypeName(groupField.type);
      const groupType = typeMap.get(groupTypeName);
      if (!groupType?.fields) continue;

      // Traverse versions (e.g., v1alpha1)
      for (const versionField of groupType.fields) {
        if (versionField.name.startsWith('__')) continue;
        const version = versionField.name;
        const versionTypeName = resolveTypeName(versionField.type);
        const versionType = typeMap.get(versionTypeName);
        if (!versionType?.fields) continue;

        // Get list fields from the query version type.
        // Exclude mutations, internal fields, and single-item getters (which have required args).
        const listFields = versionType.fields.filter((f: any) => {
          if (f.name.startsWith('create') || f.name.startsWith('update') ||
              f.name.startsWith('delete') || f.name.startsWith('__')) return false;
          // Exclude fields with required arguments (single-item getters like Foo(name: String!))
          const hasRequiredArg = f.args?.some((a: any) => a.type?.kind === 'NON_NULL');
          if (hasRequiredArg) return false;
          return true;
        });

        // Try to get kind names from mutations (may be on same or separate type)
        let createFields = versionType.fields.filter((f: any) =>
          f.name.startsWith('create')
        );

        // If no create fields on query version type, check mutation version type
        if (createFields.length === 0) {
          const mutVersionType = mutationVersionTypes.get(`${graphqlGroup}/${version}`);
          if (mutVersionType?.fields) {
            createFields = mutVersionType.fields.filter((f: any) =>
              f.name.startsWith('create')
            );
          }
        }

        const kindToPlural = new Map<string, string>();

        if (createFields.length > 0) {
          // Extract kind from create mutation names: "createFoo" → "Foo"
          const kinds = createFields.map((f: any) => f.name.slice(6));
          for (const kind of kinds) {
            // Match the plural form: must start with the kind name AND be longer
            // (e.g., "Certificates" not "Certificate")
            const listField = listFields.find(
              (f: any) =>
                f.name.toLowerCase().startsWith(kind.toLowerCase()) &&
                f.name.length > kind.length
            );
            if (listField) {
              kindToPlural.set(kind, listField.name);
            }
          }
        } else {
          // Fallback: derive kind from list field names by singularizing
          // e.g., "VirtualMachines" → "VirtualMachine", "Buckets" → "Bucket"
          for (const listField of listFields) {
            const plural = listField.name;
            let kind = plural;
            if (kind.endsWith('es') && !kind.endsWith('ses') && !kind.endsWith('ces')) {
              kind = kind.slice(0, -2);
            } else if (kind.endsWith('s')) {
              kind = kind.slice(0, -1);
            }
            kindToPlural.set(kind, plural);
          }
        }

        // Build DiscoveredResource for each kind
        for (const [kind, plural] of kindToPlural) {
          const listField = listFields.find((f: any) => f.name === plural);
          if (!listField) continue;

          // Resolve spec fields - try list type traversal first, then mutation args
          let specFields = this.resolveSpecFields(listField, typeMap);

          if (specFields.length === 0) {
            // Fallback: extract from create mutation input type
            const createField = createFields.find(
              (f: any) => f.name === `create${kind}`
            );
            if (createField) {
              specFields = this.resolveSpecFieldsFromMutation(createField, typeMap);
            }
          }

          if (specFields.length === 0) {
            // Last resort: search typeMap for spec type by naming convention
            specFields = this.resolveSpecFieldsByTypeName(kind, typeMap);
          }

          resources.push({
            group,
            version,
            kind,
            plural,
            graphqlGroup,
            category,
            specFields,
          });
        }
      }
    }

    console.log(`[GenericResourceService] Discovered ${resources.length} resources:`,
      resources.map(r => `${r.category}/${r.kind}`));
    return resources;
  }

  private resolveSpecFields(listField: any, typeMap: Map<string, any>): SpecField[] {
    const resolvedTypeName = resolveTypeName(listField.type);
    const resolvedType = typeMap.get(resolvedTypeName);
    if (!resolvedType?.fields) {
      console.warn(`[resolveSpecFields] No type for ${listField.name}, resolved: ${resolvedTypeName}`);
      return [];
    }

    // The resolved type might be:
    // 1. A list wrapper with "items" field (e.g., BucketList { items: [Bucket] })
    // 2. The item type directly (e.g., Bucket { apiVersion, kind, metadata, spec, status })
    //    This happens when resolveTypeName unwraps LIST/NON_NULL wrappers.
    let itemType = resolvedType;

    const itemsField = resolvedType.fields.find((f: any) => f.name === 'items');
    if (itemsField) {
      // Case 1: list wrapper → follow items to item type
      const itemTypeName = resolveTypeName(itemsField.type);
      const resolved = typeMap.get(itemTypeName);
      if (resolved?.fields) {
        itemType = resolved;
      }
    }
    // Case 2: already the item type (has spec directly)

    const specField = itemType.fields.find((f: any) => f.name === 'spec');
    if (!specField) {
      console.warn(`[resolveSpecFields] No 'spec' field on ${resolvedTypeName}, fields:`,
        itemType.fields.map((f: any) => f.name));
      return [];
    }

    const specTypeName = resolveTypeName(specField.type);
    const specType = typeMap.get(specTypeName);
    if (!specType?.fields) {
      console.warn(`[resolveSpecFields] No spec type for ${listField.name}, resolved: ${specTypeName}`);
      return [];
    }

    const fields = specType.fields
      .filter((f: any) => !f.name.startsWith('__'))
      .map((f: any) => {
        const { graphqlType, required } = resolveGraphQLVarType(f.type);
        return {
          name: f.name,
          graphqlType,
          scalarType: resolveScalarType(f.type),
          required,
        };
      });

    console.log(`[resolveSpecFields] ${listField.name} → spec fields:`,
      fields.map((f: SpecField) => `${f.name}(${f.graphqlType}${f.required ? '!' : ''})`));
    return fields;
  }

  /**
   * Fallback: resolve spec fields from the create mutation's input type.
   * Traverses: createMutation args → object arg → object input type → spec field → spec input type → inputFields
   */
  private resolveSpecFieldsFromMutation(createField: any, typeMap: Map<string, any>): SpecField[] {
    if (!createField) return [];

    // Find the "object" argument on the create mutation
    const objectArg = createField.args?.find((a: any) => a.name === 'object');
    if (objectArg) {
      const objectTypeName = resolveTypeName(objectArg.type);
      const objectType = typeMap.get(objectTypeName);
      console.log(`[resolveSpecFieldsFromMutation] object input type: ${objectTypeName}`,
        objectType ? `(${objectType.kind}, fields: ${(objectType.inputFields || objectType.fields || []).map((f: any) => f.name)})` : '(not found)');

      if (objectType) {
        // Look for 'spec' in inputFields (for INPUT_OBJECT types) or fields
        const allFields = objectType.inputFields || objectType.fields || [];
        const specField = allFields.find((f: any) => f.name === 'spec');
        if (specField) {
          const specTypeName = resolveTypeName(specField.type);
          const specType = typeMap.get(specTypeName);
          if (specType) {
            const specInputFields = specType.inputFields || specType.fields || [];
            const fields = specInputFields
              .filter((f: any) => !f.name.startsWith('__'))
              .map((f: any) => {
                const { graphqlType, required } = resolveGraphQLVarType(f.type);
                return {
                  name: f.name,
                  graphqlType,
                  scalarType: resolveScalarType(f.type),
                  required,
                };
              });
            if (fields.length > 0) {
              console.log(`[resolveSpecFieldsFromMutation] Resolved from mutation input:`,
                fields.map((f: SpecField) => `${f.name}(${f.graphqlType})`));
              return fields;
            }
          }
        }
      }
    }

    // Search for matching spec input types by name
    const kind = createField.name.slice(6); // "createFoo" → "Foo"
    return this.resolveSpecFieldsByTypeName(kind, typeMap);
  }

  /**
   * Last resort: find spec type by searching the type map for naming patterns.
   */
  private resolveSpecFieldsByTypeName(kind: string, typeMap: Map<string, any>): SpecField[] {
    // Try common naming patterns for spec types
    const patterns = [
      `${kind}Spec`,
      `${kind}SpecInput`,
      `${kind}_spec`,
    ];

    // Also search for types containing the kind name and "spec" (case insensitive)
    const kindLower = kind.toLowerCase();
    const candidates: Array<{ name: string; type: any }> = [];

    for (const [typeName, type] of typeMap) {
      const nameLower = typeName.toLowerCase();
      if (nameLower.includes(kindLower) && nameLower.includes('spec') && type.fields) {
        candidates.push({ name: typeName, type });
      }
    }

    // Try exact patterns first
    for (const pattern of patterns) {
      const specType = typeMap.get(pattern);
      if (specType?.fields) {
        const fields = this.extractSpecFieldsFromType(specType);
        if (fields.length > 0) {
          console.log(`[resolveSpecFieldsByTypeName] Found spec type ${pattern} for ${kind}:`,
            fields.map((f: SpecField) => f.name));
          return fields;
        }
      }
    }

    // Try candidates
    for (const candidate of candidates) {
      const fields = this.extractSpecFieldsFromType(candidate.type);
      if (fields.length > 0) {
        console.log(`[resolveSpecFieldsByTypeName] Found spec type ${candidate.name} for ${kind}:`,
          fields.map((f: SpecField) => f.name));
        return fields;
      }
    }

    console.warn(`[resolveSpecFieldsByTypeName] No spec type found for ${kind}. Candidates searched:`,
      candidates.map(c => c.name));
    return [];
  }

  private extractSpecFieldsFromType(specType: any): SpecField[] {
    const allFields = specType?.inputFields || specType?.fields;
    if (!allFields) return [];
    return allFields
      .filter((f: any) => !f.name.startsWith('__'))
      .map((f: any) => {
        const { graphqlType, required } = resolveGraphQLVarType(f.type);
        return {
          name: f.name,
          graphqlType,
          scalarType: resolveScalarType(f.type),
          required,
        };
      });
  }

  /**
   * Get service categories grouped from discovered resources.
   */
  getServiceCategories(): Observable<ServiceCategory[]> {
    return this.discoverResources().pipe(
      map((resources) => this.buildCategories(resources))
    );
  }

  private buildCategories(resources: DiscoveredResource[]): ServiceCategory[] {
    const categoryMap = new Map<string, DiscoveredResource[]>();
    for (const r of resources) {
      const list = categoryMap.get(r.category) || [];
      list.push(r);
      categoryMap.set(r.category, list);
    }

    return Array.from(categoryMap.entries())
      .map(([name, categoryResources]) => {
        const key = name.toLowerCase();
        const config = CATEGORY_CONFIG[key];
        return {
          name,
          icon: config?.icon || 'hint',
          resources: categoryResources,
          order: config?.order ?? 99,
        };
      })
      .sort((a, b) => (a as any).order - (b as any).order)
      .map(({ name, icon, resources: res }) => ({ name, icon, resources: res }));
  }

  /**
   * Find a resource by group and kind.
   */
  findResource(
    group: string,
    kind: string
  ): Observable<DiscoveredResource | undefined> {
    return this.discoverResources().pipe(
      map((resources) =>
        resources.find(
          (r) =>
            r.group === group && r.kind.toLowerCase() === kind.toLowerCase()
        )
      )
    );
  }

  // --- Generic GraphQL query builders ---

  private buildListQuery(resource: DiscoveredResource): string {
    const specFieldNames = resource.specFields.map((f) => f.name).join('\n                  ');

    // Only include spec block if there are known spec fields
    const specBlock = specFieldNames
      ? `spec {\n                  ${specFieldNames}\n                }`
      : '';

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
                ${specBlock}
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
              }
            }
          }
        }
      }
    `;
  }

  private buildCreateMutation(resource: DiscoveredResource): string {
    const specVarDecls = resource.specFields
      .map((f) => `$${f.name}: ${f.graphqlType}`)
      .join(', ');
    const specObject = resource.specFields
      .map((f) => `${f.name}: $${f.name}`)
      .join(', ');

    return `
      mutation Create${resource.kind}($namespace: String!, $name: String!${specVarDecls ? ', ' + specVarDecls : ''}) {
        ${resource.graphqlGroup} {
          ${resource.version} {
            create${resource.kind}(
              namespace: $namespace
              object: {
                metadata: { name: $name }
                spec: { ${specObject} }
              }
            ) {
              metadata { name namespace }
            }
          }
        }
      }
    `;
  }

  private buildUpdateMutation(resource: DiscoveredResource): string {
    const specVarDecls = resource.specFields
      .map((f) => `$${f.name}: ${f.graphqlType}`)
      .join(', ');
    const specObject = resource.specFields
      .map((f) => `${f.name}: $${f.name}`)
      .join(', ');

    return `
      mutation Update${resource.kind}($namespace: String!, $name: String!${specVarDecls ? ', ' + specVarDecls : ''}) {
        ${resource.graphqlGroup} {
          ${resource.version} {
            update${resource.kind}(
              namespace: $namespace
              name: $name
              object: {
                metadata: { name: $name }
                spec: { ${specObject} }
              }
            ) {
              metadata { name namespace }
            }
          }
        }
      }
    `;
  }

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

  // --- CRUD operations ---

  listResources(resource: DiscoveredResource): Observable<GenericResource[]> {
    const query = this.buildListQuery(resource);
    console.log(`[listResources] Query for ${resource.kind}:`, query);
    console.log(`[listResources] Response path: data.${resource.graphqlGroup}.${resource.version}.${resource.plural}.items`);
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
        console.log(`[listResources] Raw response for ${resource.kind}:`, JSON.stringify(response, null, 2));
        const groupData = response.data?.[resource.graphqlGroup];
        const versionData = groupData?.[resource.version];
        const resourceData = versionData?.[resource.plural];
        const items = resourceData?.items || [];
        console.log(`[listResources] ${resource.kind}: groupData=${!!groupData}, versionData=${!!versionData}, resourceData=${!!resourceData}, items=${items.length}`);
        return items;
      }),
      catchError((error) => {
        console.error(`Error fetching ${resource.plural}:`, error);
        return of([]);
      })
    );
  }

  createResource(
    resource: DiscoveredResource,
    name: string,
    namespace: string,
    spec: Record<string, any>
  ): Observable<boolean> {
    const mutation = this.buildCreateMutation(resource);
    const variables: Record<string, any> = { namespace, name, ...spec };

    return this.getGraphQLConfig().pipe(
      take(1),
      switchMap(({ endpoint, token }) =>
        from(
          fetch(endpoint, {
            method: 'POST',
            headers: this.buildHeaders(token),
            body: JSON.stringify({ query: mutation, variables }),
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
        return !!versionData?.[`create${resource.kind}`];
      }),
      catchError((error) => {
        console.error(`Error creating ${resource.kind}:`, error);
        return of(false);
      })
    );
  }

  updateResource(
    resource: DiscoveredResource,
    name: string,
    namespace: string,
    spec: Record<string, any>
  ): Observable<boolean> {
    const mutation = this.buildUpdateMutation(resource);
    const variables: Record<string, any> = { namespace, name, ...spec };

    return this.getGraphQLConfig().pipe(
      take(1),
      switchMap(({ endpoint, token }) =>
        from(
          fetch(endpoint, {
            method: 'POST',
            headers: this.buildHeaders(token),
            body: JSON.stringify({ query: mutation, variables }),
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
            body: JSON.stringify({ query: mutation, variables: { name, namespace } }),
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

  listNamespaces(): Observable<Namespace[]> {
    const query = `
      query ListNamespaces {
        v1 {
          Namespaces {
            items {
              metadata { name }
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
