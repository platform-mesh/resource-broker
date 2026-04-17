/**
 * Resource List Component
 *
 * Generic component for listing any discovered resource type.
 * Dynamically renders spec fields based on introspected schema.
 */
import { Component, CUSTOM_ELEMENTS_SCHEMA, inject, signal, OnInit } from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import * as LuigiClient from '@luigi-project/client';
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
import '@ui5/webcomponents-icons/dist/database.js';
import '@ui5/webcomponents-icons/dist/discussion.js';
import '@ui5/webcomponents-icons/dist/grid.js';
import '@ui5/webcomponents-icons/dist/lightbulb.js';
import '@ui5/webcomponents-icons/dist/chain-link.js';
import '@ui5/webcomponents-icons/dist/edit.js';

import {
  GenericResourceService,
  DiscoveredResource,
  GenericResource,
  Namespace,
  CATEGORY_ICONS,
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
  public specFormValues = signal<Record<string, any>>({});

  // Details dialog
  public showDetailsDialog = signal<boolean>(false);
  public selectedResource = signal<GenericResource | null>(null);

  // Edit mode
  public isEditMode = signal<boolean>(false);
  public editSpecValues = signal<Record<string, any>>({});

  private luigiInitialized = false;
  private pendingRouteParams: { group: string; kind: string } | null = null;

  ngOnInit(): void {
    LuigiClient.addInitListener(() => {
      this.luigiInitialized = true;
      LuigiClient.uxManager().hideLoadingIndicator();
      if (this.pendingRouteParams) {
        this.loadResourceFromParams(this.pendingRouteParams.group, this.pendingRouteParams.kind);
        this.pendingRouteParams = null;
      }
    });

    this.route.paramMap.subscribe((params) => {
      const group = params.get('group');
      const kind = params.get('kind');
      if (group && kind) {
        if (this.luigiInitialized) {
          this.loadResourceFromParams(group, kind);
        } else {
          this.pendingRouteParams = { group, kind };
        }
      }
    });
  }

  private loadResourceFromParams(group: string, kind: string): void {
    this.genericResourceService.findResource(group, kind).subscribe((resource) => {
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
    });
  }

  public loadNamespaces(): void {
    this.genericResourceService.listNamespaces().subscribe({
      next: (namespaces) => {
        this.namespaces.set(namespaces);
        if (namespaces.length > 0 && !this.newResourceNamespace()) {
          this.newResourceNamespace.set(namespaces[0].metadata.name);
        }
      },
      error: (err) => console.error('Failed to load namespaces:', err),
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
    this.specFormValues.set({});
    if (this.namespaces().length > 0) {
      this.newResourceNamespace.set(this.namespaces()[0].metadata.name);
    }
    this.showAddDialog.set(true);
  }

  public closeAddDialog(): void {
    this.showAddDialog.set(false);
  }

  public onNameInput(event: Event): void {
    this.newResourceName.set((event.target as HTMLInputElement).value);
  }

  public onNamespaceChange(event: Event): void {
    this.newResourceNamespace.set((event.target as any).selectedOption?.value || '');
  }

  public onSpecFieldInput(fieldName: string, event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    this.specFormValues.update((v) => ({ ...v, [fieldName]: value }));
  }

  public onEditSpecFieldInput(fieldName: string, event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    this.editSpecValues.update((v) => ({ ...v, [fieldName]: value }));
  }

  public confirmAddResource(): void {
    const resource = this.resource();
    if (!resource) return;

    const name = this.newResourceName().trim();
    const namespace = this.newResourceNamespace().trim();

    if (!name) {
      LuigiClient.uxManager().showAlert({ text: 'Please enter a name', type: 'warning', closeAfter: 3000 });
      return;
    }
    if (!namespace) {
      LuigiClient.uxManager().showAlert({ text: 'Please select a namespace', type: 'warning', closeAfter: 3000 });
      return;
    }

    // Validate required fields and build spec
    const spec: Record<string, any> = {};
    const formValues = this.specFormValues();
    for (const field of resource.specFields) {
      const raw = formValues[field.name];
      if (field.required && (!raw || String(raw).trim() === '')) {
        LuigiClient.uxManager().showAlert({
          text: `Please enter ${this.formatFieldName(field.name)}`,
          type: 'warning',
          closeAfter: 3000,
        });
        return;
      }
      if (raw !== undefined && raw !== '') {
        spec[field.name] = field.scalarType === 'number' ? Number(raw) : raw;
      }
    }

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
              LuigiClient.uxManager().showAlert({
                text: success
                  ? `${resource.kind} "${item.metadata.name}" deleted`
                  : `Failed to delete ${resource.kind}`,
                type: success ? 'success' : 'error',
                closeAfter: 3000,
              });
              if (success) this.loadResources();
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
      .catch(() => {});
  }

  public openDetails(item: GenericResource): void {
    this.selectedResource.set(item);
    this.isEditMode.set(false);
    const values: Record<string, any> = {};
    for (const field of this.resource()?.specFields || []) {
      values[field.name] = item.spec?.[field.name] ?? '';
    }
    this.editSpecValues.set(values);
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
      const values: Record<string, any> = {};
      for (const field of this.resource()?.specFields || []) {
        values[field.name] = selected.spec?.[field.name] ?? '';
      }
      this.editSpecValues.set(values);
    }
    this.isEditMode.set(!this.isEditMode());
  }

  public saveChanges(): void {
    const resource = this.resource();
    const selected = this.selectedResource();
    if (!resource || !selected) return;

    const spec: Record<string, any> = {};
    const editValues = this.editSpecValues();
    for (const field of resource.specFields) {
      const raw = editValues[field.name];
      if (raw !== undefined && raw !== '') {
        spec[field.name] = field.scalarType === 'number' ? Number(raw) : raw;
      }
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

  // --- Display helpers ---

  public getProviderCluster(item: GenericResource): string | null {
    const annotation = item.metadata.annotations?.['broker.platform-mesh.io/provider-cluster'];
    if (!annotation) return null;
    const parts = annotation.split('#');
    return parts.length > 1 ? parts[1] : annotation;
  }

  public getIcon(): string {
    const cat = this.resource()?.category;
    return (cat && CATEGORY_ICONS[cat]) || 'hint';
  }

  public formatFieldName(name: string): string {
    return name
      .replace(/([a-z])([A-Z])/g, '$1 $2')
      .replace(/^./, (c) => c.toUpperCase());
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
    | 'Accent1' | 'Accent2' | 'Accent3' | 'Accent4' | 'Accent5'
    | 'Accent6' | 'Accent7' | 'Accent8' | 'Accent9' | 'Accent10' {
    const schemes = [
      'Accent1', 'Accent2', 'Accent3', 'Accent4', 'Accent5',
      'Accent6', 'Accent7', 'Accent8', 'Accent9', 'Accent10',
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
        month: 'short', day: 'numeric', year: 'numeric',
        hour: '2-digit', minute: '2-digit',
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
    if (typeof relatedResources === 'string') {
      try { return JSON.parse(relatedResources); } catch { return null; }
    }
    return relatedResources;
  }

  public hasRelatedResources(item: GenericResource): boolean {
    const parsed = this.parseRelatedResources(item);
    return !!parsed && Object.keys(parsed).length > 0;
  }

  public getRelatedResourcesList(item: GenericResource): Array<{
    key: string; name: string; namespace?: string;
    gvk?: { group: string; version: string; kind: string };
  }> {
    const parsed = this.parseRelatedResources(item);
    if (!parsed) return [];
    return Object.entries(parsed).map(([key, value]: [string, any]) => ({
      key, name: value.name || 'Unknown', namespace: value.namespace, gvk: value.gvk,
    }));
  }
}
