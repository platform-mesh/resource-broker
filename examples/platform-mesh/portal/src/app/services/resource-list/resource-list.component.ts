/**
 * Resource List Component
 *
 * Generic component for listing any discovered resource type.
 * Handles CRUD operations with resource-specific forms.
 */
import { Component, CUSTOM_ELEMENTS_SCHEMA, inject, signal, OnInit } from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import LuigiClient from '@luigi-project/client';
import {
  AvatarComponent,
  ButtonComponent,
  DialogComponent,
  DynamicPageComponent,
  DynamicPageHeaderComponent,
  DynamicPageTitleComponent,
  IconComponent,
  InputComponent,
  LabelComponent,
  OptionComponent,
  SelectComponent,
  TextComponent,
  TitleComponent,
  ToolbarButtonComponent,
  ToolbarComponent,
} from '@ui5/webcomponents-ngx';

import '@ui5/webcomponents-icons/dist/add.js';
import '@ui5/webcomponents-icons/dist/calendar.js';
import '@ui5/webcomponents-icons/dist/delete.js';
import '@ui5/webcomponents-icons/dist/connected.js';
import '@ui5/webcomponents-icons/dist/disconnected.js';
import '@ui5/webcomponents-icons/dist/refresh.js';
import '@ui5/webcomponents-icons/dist/hint.js';
import '@ui5/webcomponents-icons/dist/it-host.js';
import '@ui5/webcomponents-icons/dist/locked.js';
import '@ui5/webcomponents-icons/dist/chain-link.js';
import '@ui5/webcomponents-icons/dist/edit.js';

import {
  GenericResourceService,
  DiscoveredResource,
  GenericResource,
  Namespace,
} from '../generic-resource.service';

@Component({
  selector: 'app-resource-list',
  standalone: true,
  imports: [
    DynamicPageComponent,
    DynamicPageTitleComponent,
    DynamicPageHeaderComponent,
    AvatarComponent,
    TitleComponent,
    LabelComponent,
    TextComponent,
    ToolbarComponent,
    ToolbarButtonComponent,
    IconComponent,
    InputComponent,
    ButtonComponent,
    DialogComponent,
    SelectComponent,
    OptionComponent,
  ],
  schemas: [CUSTOM_ELEMENTS_SCHEMA],
  templateUrl: './resource-list.component.html',
  styleUrl: './resource-list.component.scss',
})
export class ResourceListComponent implements OnInit {
  private route = inject(ActivatedRoute);
  private genericResourceService = inject(GenericResourceService);

  public resource = signal<DiscoveredResource | null>(null);
  public resources = signal<GenericResource[]>([]);
  public namespaces = signal<Namespace[]>([]);
  public loading = signal<boolean>(true);

  // Add resource dialog
  public showAddDialog = signal<boolean>(false);
  public newResourceName = signal<string>('');
  public newResourceNamespace = signal<string>('');

  // VM-specific fields
  public newVmArch = signal<string>('x86_64');
  public newVmMemory = signal<number>(512);

  // Certificate-specific fields
  public newCertFqdn = signal<string>('');

  // Details dialog
  public showDetailsDialog = signal<boolean>(false);
  public selectedResource = signal<GenericResource | null>(null);

  // Edit mode
  public isEditMode = signal<boolean>(false);
  public editCertFqdn = signal<string>('');

  private luigiInitialized = false;
  private pendingRouteParams: { group: string; kind: string } | null = null;

  ngOnInit(): void {
    // Wait for Luigi to initialize before loading data
    LuigiClient.addInitListener(() => {
      this.luigiInitialized = true;
      LuigiClient.uxManager().hideLoadingIndicator();

      // If we have pending route params, load them now
      if (this.pendingRouteParams) {
        this.loadResourceFromParams(this.pendingRouteParams.group, this.pendingRouteParams.kind);
        this.pendingRouteParams = null;
      }
    });

    // Subscribe to route param changes
    this.route.paramMap.subscribe((params) => {
      const group = params.get('group');
      const kind = params.get('kind');

      if (group && kind) {
        if (this.luigiInitialized) {
          this.loadResourceFromParams(group, kind);
        } else {
          // Store params to load after Luigi initializes
          this.pendingRouteParams = { group, kind };
        }
      }
    });
  }

  private loadResourceFromParams(group: string, kind: string): void {
    const resource = this.genericResourceService.findResource(group, kind);
    if (resource) {
      this.resource.set(resource);
      this.loadNamespaces();
      this.loadResources();
    } else {
      LuigiClient.uxManager().showAlert({
        text: `Resource type not found: ${group}/${kind}`,
        type: 'error',
        closeAfter: 3000,
      });
    }
  }

  public loadNamespaces(): void {
    this.genericResourceService.listNamespaces().subscribe({
      next: (namespaces) => {
        this.namespaces.set(namespaces);
        if (namespaces.length > 0 && !this.newResourceNamespace()) {
          this.newResourceNamespace.set(namespaces[0].metadata.name);
        }
      },
      error: (err) => {
        console.error('Failed to load namespaces:', err);
      },
    });
  }

  public loadResources(): void {
    const resource = this.resource();
    if (!resource) return;

    this.loading.set(true);
    this.genericResourceService.listResources(resource).subscribe({
      next: (resources) => {
        this.resources.set(resources);
        this.loading.set(false);
        LuigiClient.uxManager().hideLoadingIndicator();
      },
      error: (err) => {
        console.error('Failed to load resources:', err);
        this.loading.set(false);
        LuigiClient.uxManager().hideLoadingIndicator();
        LuigiClient.uxManager().showAlert({
          text: `Failed to load ${resource.plural}`,
          type: 'error',
          closeAfter: 3000,
        });
      },
    });
  }

  public openAddDialog(): void {
    this.newResourceName.set('');
    this.newVmArch.set('x86_64');
    this.newVmMemory.set(512);
    this.newCertFqdn.set('');
    if (this.namespaces().length > 0) {
      this.newResourceNamespace.set(this.namespaces()[0].metadata.name);
    }
    this.showAddDialog.set(true);
  }

  public closeAddDialog(): void {
    this.showAddDialog.set(false);
  }

  public onNameInput(event: Event): void {
    const input = event.target as HTMLInputElement;
    this.newResourceName.set(input.value);
  }

  public onNamespaceChange(event: Event): void {
    const select = event.target as any;
    this.newResourceNamespace.set(select.selectedOption?.value || '');
  }

  public onArchChange(event: Event): void {
    const select = event.target as any;
    this.newVmArch.set(select.selectedOption?.value || 'x86_64');
  }

  public onMemoryInput(event: Event): void {
    const input = event.target as HTMLInputElement;
    this.newVmMemory.set(parseInt(input.value, 10) || 512);
  }

  public onFqdnInput(event: Event): void {
    const input = event.target as HTMLInputElement;
    this.newCertFqdn.set(input.value);
  }

  public confirmAddResource(): void {
    const resource = this.resource();
    if (!resource) return;

    const name = this.newResourceName().trim();
    const namespace = this.newResourceNamespace().trim();

    if (!name) {
      LuigiClient.uxManager().showAlert({
        text: 'Please enter a name',
        type: 'warning',
        closeAfter: 3000,
      });
      return;
    }

    if (!namespace) {
      LuigiClient.uxManager().showAlert({
        text: 'Please select a namespace',
        type: 'warning',
        closeAfter: 3000,
      });
      return;
    }

    // Build spec based on resource type
    let spec: Record<string, any> = {};

    if (resource.kind === 'VirtualMachine') {
      spec = {
        arch: this.newVmArch(),
        memory: this.newVmMemory(),
      };
    } else if (resource.kind === 'Certificate') {
      const fqdn = this.newCertFqdn().trim();
      if (!fqdn) {
        LuigiClient.uxManager().showAlert({
          text: 'Please enter an FQDN',
          type: 'warning',
          closeAfter: 3000,
        });
        return;
      }
      spec = { fqdn };
    }

    console.log('[ResourceList] Creating resource:', { kind: resource.kind, name, namespace, spec });
    this.genericResourceService.createResource(resource, name, namespace, spec).subscribe({
      next: (success) => {
        if (success) {
          LuigiClient.uxManager().showAlert({
            text: `${resource.kind} "${name}" created successfully`,
            type: 'success',
            closeAfter: 3000,
          });
          this.closeAddDialog();
          this.loadResources();
        } else {
          LuigiClient.uxManager().showAlert({
            text: `Failed to create ${resource.kind}`,
            type: 'error',
            closeAfter: 3000,
          });
        }
      },
      error: () => {
        LuigiClient.uxManager().showAlert({
          text: `Failed to create ${resource.kind}`,
          type: 'error',
          closeAfter: 3000,
        });
      },
    });
  }

  public deleteResource(item: GenericResource): void {
    const resource = this.resource();
    if (!resource) return;

    LuigiClient.uxManager()
      .showConfirmationModal({
        type: 'warning',
        header: `Delete ${resource.kind}`,
        body: `Are you sure you want to delete "${item.metadata.name}"?`,
        buttonConfirm: 'Delete',
        buttonDismiss: 'Cancel',
      })
      .then(() => {
        this.genericResourceService
          .deleteResource(resource, item.metadata.name, item.metadata.namespace || 'default')
          .subscribe({
            next: (success) => {
              if (success) {
                LuigiClient.uxManager().showAlert({
                  text: `${resource.kind} "${item.metadata.name}" deleted`,
                  type: 'success',
                  closeAfter: 3000,
                });
                this.loadResources();
              } else {
                LuigiClient.uxManager().showAlert({
                  text: `Failed to delete ${resource.kind}`,
                  type: 'error',
                  closeAfter: 3000,
                });
              }
            },
            error: () => {
              LuigiClient.uxManager().showAlert({
                text: `Failed to delete ${resource.kind}`,
                type: 'error',
                closeAfter: 3000,
              });
            },
          });
      })
      .catch(() => {
        console.log('Delete cancelled');
      });
  }

  public openDetails(item: GenericResource): void {
    this.selectedResource.set(item);
    this.isEditMode.set(false);
    // Initialize edit fields with current values
    if (this.isCertificate()) {
      this.editCertFqdn.set(item.spec?.['fqdn'] || '');
    }
    this.showDetailsDialog.set(true);
  }

  public closeDetailsDialog(): void {
    this.showDetailsDialog.set(false);
    this.selectedResource.set(null);
    this.isEditMode.set(false);
  }

  public toggleEditMode(): void {
    const selected = this.selectedResource();
    if (!selected) return;

    if (!this.isEditMode()) {
      // Entering edit mode - initialize fields
      if (this.isCertificate()) {
        this.editCertFqdn.set(selected.spec?.['fqdn'] || '');
      }
    }
    this.isEditMode.set(!this.isEditMode());
  }

  public onEditFqdnInput(event: Event): void {
    const input = event.target as HTMLInputElement;
    this.editCertFqdn.set(input.value);
  }

  public saveChanges(): void {
    const resource = this.resource();
    const selected = this.selectedResource();
    if (!resource || !selected) return;

    let spec: Record<string, any> = {};

    if (resource.kind === 'Certificate') {
      const fqdn = this.editCertFqdn().trim();
      if (!fqdn) {
        LuigiClient.uxManager().showAlert({
          text: 'Please enter an FQDN',
          type: 'warning',
          closeAfter: 3000,
        });
        return;
      }
      spec = { fqdn };
    }

    this.genericResourceService
      .updateResource(resource, selected.metadata.name, selected.metadata.namespace || 'default', spec)
      .subscribe({
        next: (success) => {
          if (success) {
            LuigiClient.uxManager().showAlert({
              text: `${resource.kind} "${selected.metadata.name}" updated successfully`,
              type: 'success',
              closeAfter: 3000,
            });
            this.isEditMode.set(false);
            this.closeDetailsDialog();
            this.loadResources();
          } else {
            LuigiClient.uxManager().showAlert({
              text: `Failed to update ${resource.kind}`,
              type: 'error',
              closeAfter: 3000,
            });
          }
        },
        error: () => {
          LuigiClient.uxManager().showAlert({
            text: `Failed to update ${resource.kind}`,
            type: 'error',
            closeAfter: 3000,
          });
        },
      });
  }

  public getProviderCluster(item: GenericResource): string | null {
    const annotation = item.metadata.annotations?.['broker.platform-mesh.io/provider-cluster'];
    if (!annotation) return null;
    // Format: "provider#2y0t8i3qh5kcn20c" -> extract just the ID part after #
    const parts = annotation.split('#');
    return parts.length > 1 ? parts[1] : annotation;
  }

  public isVirtualMachine(): boolean {
    return this.resource()?.kind === 'VirtualMachine';
  }

  public isCertificate(): boolean {
    return this.resource()?.kind === 'Certificate';
  }

  public getIcon(): string {
    const resource = this.resource();
    if (resource?.category === 'Compute') return 'it-host';
    if (resource?.category === 'PKI') return 'locked';
    return 'hint';
  }

  public getInitials(name: string): string {
    if (!name) return '??';
    const parts = name.split(/[-_\s]+/);
    if (parts.length >= 2) {
      return (parts[0][0] + parts[1][0]).toUpperCase();
    }
    return name.substring(0, 2).toUpperCase();
  }

  public getColorScheme(
    name: string
  ):
    | 'Accent1'
    | 'Accent2'
    | 'Accent3'
    | 'Accent4'
    | 'Accent5'
    | 'Accent6'
    | 'Accent7'
    | 'Accent8'
    | 'Accent9'
    | 'Accent10' {
    const schemes = [
      'Accent1',
      'Accent2',
      'Accent3',
      'Accent4',
      'Accent5',
      'Accent6',
      'Accent7',
      'Accent8',
      'Accent9',
      'Accent10',
    ] as const;
    let hash = 0;
    for (let i = 0; i < name.length; i++) {
      hash = name.charCodeAt(i) + ((hash << 5) - hash);
    }
    return schemes[Math.abs(hash) % schemes.length];
  }

  public getConditionClass(status: string): string {
    if (status === 'True') return 'success';
    if (status === 'False') return 'error';
    return 'pending';
  }

  public formatDate(timestamp: string | undefined): string {
    if (!timestamp) return 'Unknown';
    try {
      const date = new Date(timestamp);
      return date.toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      });
    } catch {
      return timestamp;
    }
  }

  public getSpecValue(item: GenericResource, key: string): string {
    const value = item.spec?.[key];
    if (value === undefined || value === null) return '-';
    return String(value);
  }

  private parseRelatedResources(item: GenericResource): Record<string, any> | null {
    const relatedResources = item.status?.relatedResources;
    if (!relatedResources) return null;

    // GraphQL returns relatedResources as a JSON scalar string, so we need to parse it
    if (typeof relatedResources === 'string') {
      try {
        return JSON.parse(relatedResources);
      } catch {
        console.error('Failed to parse relatedResources:', relatedResources);
        return null;
      }
    }

    // Already an object
    return relatedResources;
  }

  public hasRelatedResources(item: GenericResource): boolean {
    const parsed = this.parseRelatedResources(item);
    return !!parsed && Object.keys(parsed).length > 0;
  }

  public getRelatedResourcesList(item: GenericResource): Array<{
    key: string;
    name: string;
    namespace?: string;
    gvk?: { group: string; version: string; kind: string };
  }> {
    const parsed = this.parseRelatedResources(item);
    if (!parsed) return [];

    return Object.entries(parsed).map(([key, value]: [string, any]) => ({
      key,
      name: value.name || 'Unknown',
      namespace: value.namespace,
      gvk: value.gvk,
    }));
  }
}
