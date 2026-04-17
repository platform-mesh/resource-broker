/**
 * Services Component
 *
 * Main layout with side navigation for dynamically discovered service categories.
 * Shows available resource types organized by category in a side menu.
 */
import { Component, CUSTOM_ELEMENTS_SCHEMA, inject, signal, OnInit } from '@angular/core';
import { Router, RouterOutlet, ActivatedRoute } from '@angular/router';
import * as LuigiClient from '@luigi-project/client';
import {
  IconComponent,
  LabelComponent,
  TextComponent,
  TitleComponent,
} from '@ui5/webcomponents-ngx';

import '@ui5/webcomponents-icons/dist/it-host.js';
import '@ui5/webcomponents-icons/dist/locked.js';
import '@ui5/webcomponents-icons/dist/connected.js';
import '@ui5/webcomponents-icons/dist/cloud.js';
import '@ui5/webcomponents-icons/dist/database.js';
import '@ui5/webcomponents-icons/dist/discussion.js';
import '@ui5/webcomponents-icons/dist/grid.js';
import '@ui5/webcomponents-icons/dist/lightbulb.js';
import '@ui5/webcomponents-icons/dist/hint.js';
import '@ui5/webcomponents-icons/dist/navigation-right-arrow.js';
import '@ui5/webcomponents-icons/dist/navigation-down-arrow.js';
import '@ui5/webcomponents-icons/dist/customer.js';

import { GenericResourceService, ServiceCategory, DiscoveredResource } from './generic-resource.service';

@Component({
  selector: 'app-services',
  standalone: true,
  imports: [
    RouterOutlet,
    TitleComponent,
    LabelComponent,
    TextComponent,
    IconComponent,
  ],
  schemas: [CUSTOM_ELEMENTS_SCHEMA],
  templateUrl: './services.component.html',
  styleUrl: './services.component.scss',
})
export class ServicesComponent implements OnInit {
  private router = inject(Router);
  private route = inject(ActivatedRoute);
  private genericResourceService = inject(GenericResourceService);

  public categories = signal<ServiceCategory[]>([]);
  public loading = signal<boolean>(true);
  public selectedResource = signal<DiscoveredResource | null>(null);
  public expandedCategories = signal<Set<string>>(new Set());

  ngOnInit(): void {
    LuigiClient.addInitListener(() => {
      LuigiClient.uxManager().showLoadingIndicator();
      this.loadCategories();
    });
  }

  private loadCategories(): void {
    this.genericResourceService.getServiceCategories().subscribe({
      next: (categories) => {
        this.categories.set(categories);
        this.loading.set(false);
        // Keep all categories collapsed by default
        // Auto-select first resource if none selected
        if (categories.length > 0 && categories[0].resources.length > 0) {
          this.selectResource(categories[0].resources[0]);
        }
        LuigiClient.uxManager().hideLoadingIndicator();
      },
      error: (err) => {
        console.error('Failed to load categories:', err);
        this.loading.set(false);
        LuigiClient.uxManager().hideLoadingIndicator();
      },
    });
  }

  public selectResource(resource: DiscoveredResource): void {
    this.selectedResource.set(resource);
    this.router.navigate([resource.group, resource.kind.toLowerCase()], { relativeTo: this.route });
  }

  public isResourceSelected(resource: DiscoveredResource): boolean {
    const selected = this.selectedResource();
    return selected?.group === resource.group && selected?.kind === resource.kind;
  }

  public toggleCategory(categoryName: string): void {
    const expanded = new Set(this.expandedCategories());
    if (expanded.has(categoryName)) {
      expanded.delete(categoryName);
    } else {
      expanded.add(categoryName);
    }
    this.expandedCategories.set(expanded);
  }

  public isCategoryExpanded(categoryName: string): boolean {
    return this.expandedCategories().has(categoryName);
  }
}
