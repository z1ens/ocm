package operator

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Klusterlet Singleton mode", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var agentNamespace string
	var registrationManagementRoleName string
	var registrationManagedRoleName string
	var deploymentName string
	var saName string
	var workManagementRoleName string
	var workManagedRoleName string

	ginkgo.BeforeEach(func() {
		var ctx context.Context
		klusterlet = &operatorapiv1.Klusterlet{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("klusterlet-%s", rand.String(6)),
			},
			Spec: operatorapiv1.KlusterletSpec{
				ImagePullSpec: "quay.io/open-cluster-management/registration-operator",
				ExternalServerURLs: []operatorapiv1.ServerURL{
					{
						URL: "https://localhost",
					},
				},
				ClusterName: "testcluster",
				DeployOption: operatorapiv1.KlusterletDeployOption{
					Mode: operatorapiv1.InstallModeSingleton,
				},
			},
		}
		agentNamespace = helpers.AgentNamespace(klusterlet)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: agentNamespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ctx, cancel = context.WithCancel(context.Background())
		go startKlusterletOperator(ctx)
	})

	ginkgo.AfterEach(func() {
		err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), agentNamespace, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		if cancel != nil {
			cancel()
		}
	})

	ginkgo.Context("Deploy and clean klusterlet component", func() {
		ginkgo.BeforeEach(func() {
			deploymentName = fmt.Sprintf("%s-agent", klusterlet.Name)
			registrationManagementRoleName = fmt.Sprintf("open-cluster-management:management:%s-registration:agent", klusterlet.Name)
			workManagementRoleName = fmt.Sprintf("open-cluster-management:management:%s-work:agent", klusterlet.Name)
			registrationManagedRoleName = fmt.Sprintf("open-cluster-management:%s-registration:agent", klusterlet.Name)
			workManagedRoleName = fmt.Sprintf("open-cluster-management:%s-work:agent", klusterlet.Name)
			saName = fmt.Sprintf("%s-work-sa", klusterlet.Name)
		})

		ginkgo.AfterEach(func() {
			gomega.Expect(operatorClient.OperatorV1().Klusterlets().Delete(context.Background(), klusterlet.Name, metav1.DeleteOptions{})).To(gomega.BeNil())
		})

		ginkgo.It("should have expected resource created successfully", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Check if relatedResources are correct
			gomega.Eventually(func() error {
				actual, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), klusterlet.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				fmt.Printf("related resources are %v\n", actual.Status.RelatedResources)

				// 10 managed static manifests + 9 management static manifests + 2CRDs + 1 deployments
				if len(actual.Status.RelatedResources) != 22 {
					return fmt.Errorf("should get 22 relatedResources, actual got %v", len(actual.Status.RelatedResources))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check CRDs
			gomega.Eventually(func() bool {
				if _, err := apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(
					context.Background(), "appliedmanifestworks.work.open-cluster-management.io", metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(
					context.Background(), "clusterclaims.cluster.open-cluster-management.io", metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check clusterrole/clusterrolebinding
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), registrationManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), workManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), registrationManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), workManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check role/rolebinding
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().Roles(agentNamespace).Get(context.Background(), registrationManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().Roles(agentNamespace).Get(context.Background(), workManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().RoleBindings(agentNamespace).Get(context.Background(), registrationManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().RoleBindings(agentNamespace).Get(context.Background(), workManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			// Check extension apiserver rolebinding
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().RoleBindings("kube-system").Get(context.Background(), registrationManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().RoleBindings("kube-system").Get(context.Background(), workManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check service account
			gomega.Eventually(func() bool {
				sa, err := kubeClient.CoreV1().ServiceAccounts(agentNamespace).Get(context.Background(), saName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				_, present := sa.ObjectMeta.Annotations[util.IrsaAnnotationKey]
				return !present
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check deployment
			gomega.Eventually(func() bool {
				deployment, err := kubeClient.AppsV1().Deployments(agentNamespace).Get(context.Background(), deploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				return !util.AllCommandLineOptionsPresent(*deployment) && !util.AwsCliSpecificVolumesMounted(*deployment)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check addon namespace
			gomega.Eventually(func() bool {
				if _, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), helpers.DefaultAddonNamespace, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
		})
	})
})
