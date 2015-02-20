var ipam = angular.module('ipam',['ui.bootstrap']);

ipam.controller('IPAM', function($scope, $http) {
    $scope.allocs = [];
    $scope.dostuff = function() {
        alert("lol");
    };
    
    $http.get('/api/list').success(function(data) {
        $scope.name = data.Name;
        $scope.allocs = data.Allocs;
    });
});

ipam.directive('ipblocks', function($compile) {
    return {
        restrict: 'E',
        scope: {
            allocs: '=',
            delete: '&'
        },
        templateUrl: "alloc.html",
        // Black magic to alloc <ipblocks> to be recursive.
        compile: function(tElement, tAttr) {
            var contents = tElement.contents().remove();
            var compiledContents;
            return function(scope, iElement, iAttr) {
                if(!compiledContents) {
                    compiledContents = $compile(contents);
                }
                compiledContents(scope, function(clone, scope) {
                         iElement.append(clone); 
                });
            };
        }
    };
});
